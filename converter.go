package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func getFFmpegPath() string {
	if p := os.Getenv("FFMPEG_PATH"); p != "" {
		return p
	}
	return "ffmpeg"
}

func getTempDir() string {
	if d := os.Getenv("TEMP_DIR"); d != "" {
		return d
	}
	return "/tmp/hls-conversions"
}

func getCallbackURL() string {
	if u := os.Getenv("CALLBACK_URL"); u != "" {
		return u
	}
	return "http://localhost:8000/api/hls/callback"
}

func processJob(job *ConversionJob) {
	req := job.Request
	tempDir := filepath.Join(getTempDir(), job.ID)

	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Printf("[CONVERTER] Erro ao criar diretório temp %s: %v", tempDir, err)
		for _, q := range req.Qualities {
			sendCallback(getCallbackURL(), CallbackPayload{
				MediaID:      req.MediaFileID,
				Quality:      q,
				Status:       "failed",
				ErrorMessage: fmt.Sprintf("erro ao criar diretório temporário: %v", err),
			})
		}
		return
	}
	defer func() {
		log.Printf("[CONVERTER] Limpando diretório temp %s", tempDir)
		os.RemoveAll(tempDir)
	}()

	s3c, err := NewS3Client()
	if err != nil {
		log.Printf("[CONVERTER] Erro ao criar cliente S3: %v", err)
		for _, q := range req.Qualities {
			sendCallback(getCallbackURL(), CallbackPayload{
				MediaID:      req.MediaFileID,
				Quality:      q,
				Status:       "failed",
				ErrorMessage: fmt.Sprintf("erro ao criar cliente S3: %v", err),
			})
		}
		return
	}

	// Download original file once
	originalPath := filepath.Join(tempDir, "original"+filepath.Ext(req.S3Path))
	log.Printf("[CONVERTER] Job %s: Baixando arquivo original de %s", job.ID, req.S3Path)

	if err := s3c.Download(job.Ctx, req.S3Path, originalPath); err != nil {
		log.Printf("[CONVERTER] Erro ao baixar original: %v", err)
		for _, q := range req.Qualities {
			sendCallback(getCallbackURL(), CallbackPayload{
				MediaID:      req.MediaFileID,
				Quality:      q,
				Status:       "failed",
				ErrorMessage: fmt.Sprintf("erro ao baixar arquivo original: %v", err),
			})
		}
		return
	}

	// ✅ Download watermark se configurado
	watermarkPath := ""
	if req.Watermark != nil && req.Watermark.Enabled && req.Watermark.S3Path != "" {
		watermarkPath = filepath.Join(tempDir, "watermark"+filepath.Ext(req.Watermark.S3Path))
		log.Printf("[CONVERTER] Job %s: Baixando watermark de %s", job.ID, req.Watermark.S3Path)
		
		if err := s3c.Download(job.Ctx, req.Watermark.S3Path, watermarkPath); err != nil {
			log.Printf("[CONVERTER] Aviso: erro ao baixar watermark: %v", err)
			watermarkPath = "" // Continua sem watermark
		}
	}

	// Process each quality sequentially
	for _, quality := range req.Qualities {
		select {
		case <-job.Ctx.Done():
			log.Printf("[CONVERTER] Job %s cancelado antes de processar %s", job.ID, quality)
			return
		default:
		}

		log.Printf("[CONVERTER] Job %s: Iniciando conversão para %s", job.ID, quality)
		err := convertQuality(job, s3c, originalPath, watermarkPath, tempDir, quality)
		if err != nil {
			log.Printf("[CONVERTER] Job %s: Erro na conversão %s: %v", job.ID, quality, err)
			sendCallback(getCallbackURL(), CallbackPayload{
				MediaID:      req.MediaFileID,
				Quality:      quality,
				Status:       "failed",
				ErrorMessage: err.Error(),
			})
			continue
		}

		job.Mu.Lock()
		job.CompletedQualities = append(job.CompletedQualities, quality)
		completedQualities := make([]string, len(job.CompletedQualities))
		copy(completedQualities, job.CompletedQualities)
		job.Mu.Unlock()

		// Generate/update master playlist with all completed qualities
		if err := generateAndUploadMasterPlaylist(job.Ctx, s3c, tempDir, req.MediaFileID, completedQualities); err != nil {
			log.Printf("[CONVERTER] Job %s: Erro ao gerar master playlist: %v", job.ID, err)
		}

		qualityS3Path := fmt.Sprintf("hls/%d/%s/master.m3u8", req.MediaFileID, quality)
		log.Printf("[CONVERTER] Job %s: Conversão %s concluída. S3: %s", job.ID, quality, qualityS3Path)

		sendCallback(getCallbackURL(), CallbackPayload{
			MediaID: req.MediaFileID,
			Quality: quality,
			Status:  "completed",
			S3Path:  qualityS3Path,
		})

		// Clean up quality temp files
		qualityDir := filepath.Join(tempDir, quality)
		os.RemoveAll(qualityDir)
		log.Printf("[CONVERTER] Job %s: Arquivos temporários de %s limpos", job.ID, quality)
	}

	log.Printf("[CONVERTER] Job %s: Todas as qualidades processadas", job.ID)
}

func convertQuality(job *ConversionJob, s3c *S3Client, originalPath string, watermarkPath string, tempDir string, quality string) error {
	settings, ok := QualityMap[quality]
	if !ok {
		return fmt.Errorf("qualidade desconhecida: %s", quality)
	}

	qualityDir := filepath.Join(tempDir, quality)
	if err := os.MkdirAll(qualityDir, 0755); err != nil {
		return fmt.Errorf("erro ao criar diretório de qualidade: %w", err)
	}

	outputPlaylist := filepath.Join(qualityDir, "master.m3u8")
	segmentPattern := filepath.Join(qualityDir, "segment_%03d.ts")

	// ✅ Calcula GOP dinâmico baseado no FPS ou usa padrão
	gopSize := 60 // padrão 60 (30fps * 2)
	if job.Request.GOPSize > 0 {
		gopSize = job.Request.GOPSize
	}

	// ✅ Monta filtro de watermark se configurado
	watermarkFilter := ""
	if watermarkPath != "" && job.Request.Watermark != nil && job.Request.Watermark.Enabled {
		wm := job.Request.Watermark
		
		// Calcula posição
		position := getPositionFilter(wm.Position, wm.Size)
		
		// Calcula opacidade (0-100 para 0.0-1.0)
		opacity := wm.Opacity / 100.0
		if opacity < 0 {
			opacity = 0
		}
		if opacity > 1 {
			opacity = 1
		}

		// Monta filtro complexo de watermark
		watermarkFilter = fmt.Sprintf(
			"[1:v]format=rgba,scale=iw*%d/100:-1,coloralpha=0x000000@%.2f[wm];[0:v][wm]overlay=%s:eof_action=pass[outv];",
			wm.Size,
			opacity,
			position,
		)
	}

	// ✅ Monta args do ffmpeg
	args := []string{
		"-i", originalPath,
	}

	// Adiciona watermark se existir
	if watermarkPath != "" && watermarkFilter != "" {
		args = append(args, "-i", watermarkPath)
	}

	// Filtros de vídeo
	videoFilters := []string{
		fmt.Sprintf("scale=%s", settings.Scale),
	}

	// Adiciona filtro de watermark se existir
	if watermarkFilter != "" {
		videoFilters = append(videoFilters, watermarkFilter)
	}

	// Monta filtro final
	vf := strings.Join(videoFilters, ",")
	if watermarkFilter != "" {
		// Usa output do filtro de watermark
		args = append(args,
			"-filter_complex", vf,
			"-map", "[outv]",
		)
	} else {
		args = append(args, "-vf", vf)
	}

	// Configurações de vídeo
	args = append(args,
		"-c:v", "libx264",
		"-preset", "medium",
		"-crf", "22",
		"-maxrate", "3000k",
		"-bufsize", "6000k",
		"-profile:v", "high",
		"-level", "4.1",
		"-pix_fmt", "yuv420p",
		"-g", strconv.Itoa(gopSize),
		"-keyint_min", strconv.Itoa(gopSize),
		"-sc_threshold", "0",
	)

	// Configurações de áudio
	args = append(args,
		"-c:a", "aac",
		"-b:a", "128k",
		"-ac", "2",
	)

	// Configurações HLS
	args = append(args,
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_playlist_type", "vod",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", segmentPattern,
		outputPlaylist,
	)

	log.Printf("[FFMPEG] Executando: %s %s", getFFmpegPath(), strings.Join(args, " "))
	start := time.Now()

	cmd := exec.CommandContext(job.Ctx, getFFmpegPath(), args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if job.Ctx.Err() != nil {
			return fmt.Errorf("conversão cancelada")
		}
		return fmt.Errorf("ffmpeg falhou: %v - %s", err, stderr.String())
	}

	elapsed := time.Since(start)
	log.Printf("[FFMPEG] Conversão %s concluída em %s", quality, elapsed)

	// Upload HLS files to S3
	s3Prefix := fmt.Sprintf("hls/%d/%s", job.Request.MediaFileID, quality)
	if err := s3c.UploadDirectory(job.Ctx, qualityDir, s3Prefix); err != nil {
		return fmt.Errorf("erro ao enviar para S3: %w", err)
	}

	return nil
}

// getPositionFilter retorna o filtro de posição para overlay
func getPositionFilter(position string, size int) string {
	// size é porcentagem da largura do vídeo
	// Calcula margem como 2% da largura
	margin := "W*0.02"
	
	switch position {
	case "top-left":
		return fmt.Sprintf("%s:%s", margin, margin)
	case "top-right":
		return fmt.Sprintf("W-w-%s:%s", margin, margin)
	case "bottom-left":
		return fmt.Sprintf("%s:H-h-%s", margin, margin)
	case "center":
		return fmt.Sprintf("(W-w)/2:(H-h)/2")
	case "bottom-right":
		fallthrough
	default:
		return fmt.Sprintf("W-w-%s:H-h-%s", margin, margin)
	}
}

func generateAndUploadMasterPlaylist(ctx context.Context, s3c *S3Client, tempDir string, mediaFileID int, completedQualities []string) error {
	// Sort qualities by bandwidth for consistent ordering
	sort.Slice(completedQualities, func(i, j int) bool {
		return QualityBandwidth[completedQualities[i]] < QualityBandwidth[completedQualities[j]]
	})

	var builder strings.Builder
	builder.WriteString("#EXTM3U\n")
	builder.WriteString("#EXT-X-VERSION:3\n")

	for _, q := range completedQualities {
		bandwidth := QualityBandwidth[q]
		resolution := QualityResolution[q]
		builder.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n", bandwidth, resolution))
		builder.WriteString(fmt.Sprintf("%s/master.m3u8\n", q))
	}

	masterPath := filepath.Join(tempDir, "master.m3u8")
	if err := os.WriteFile(masterPath, []byte(builder.String()), 0644); err != nil {
		return fmt.Errorf("erro ao escrever master playlist: %w", err)
	}

	s3Key := fmt.Sprintf("hls/%d/master.m3u8", mediaFileID)
	return s3c.Upload(ctx, masterPath, s3Key)
}

func sendCallback(callbackURL string, payload CallbackPayload) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[CALLBACK] Erro ao serializar payload: %v", err)
		return
	}

	log.Printf("[CALLBACK] Enviando para %s: %s", callbackURL, string(body))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[CALLBACK] Erro ao criar request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[CALLBACK] Erro ao enviar callback: %v", err)
		return
	}
	defer resp.Body.Close()

	log.Printf("[CALLBACK] Resposta: %d", resp.StatusCode)
}
