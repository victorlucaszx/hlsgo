package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Client struct {
	client *s3.Client
	bucket string
}

func NewS3Client() (*S3Client, error) {
	bucket := os.Getenv("AWS_BUCKET")
	if bucket == "" {
		return nil, fmt.Errorf("AWS_BUCKET não configurado")
	}

	region := os.Getenv("AWS_DEFAULT_REGION")
	if region == "" {
		region = "us-east-1"
	}

	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	var cfg aws.Config
	var err error

	if accessKey != "" && secretKey != "" {
		cfg, err = config.LoadDefaultConfig(context.Background(),
			config.WithRegion(region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		)
	} else {
		cfg, err = config.LoadDefaultConfig(context.Background(),
			config.WithRegion(region),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("erro ao carregar configuração AWS: %w", err)
	}

	client := s3.NewFromConfig(cfg)
	return &S3Client{client: client, bucket: bucket}, nil
}

func (s *S3Client) Download(ctx context.Context, s3Path string, localPath string) error {
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("erro ao criar diretório %s: %w", dir, err)
	}

	// Tentativa de decodificar o s3Path, caso ele já venha com URL encoding.
	// O SDK do S3 espera a chave "crua" (não codificada), pois ele mesmo faz a codificação.
	decodedS3Path, err := url.PathUnescape(s3Path)
	if err != nil {
		// Se a decodificação falhar, usar o caminho original e logar um aviso.
		log.Printf("[S3] Aviso: falha ao decodificar s3Path '%s': %v. Usando o caminho original.", s3Path, err)
		decodedS3Path = s3Path
	}

	log.Printf("[S3] Baixando s3://%s/%s -> %s", s.bucket, decodedS3Path, localPath)

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(decodedS3Path),
	})
	if err != nil {
		return fmt.Errorf("erro ao baixar de S3: %w", err)
	}
	defer result.Body.Close()

	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("erro ao criar arquivo local: %w", err)
	}
	defer file.Close()

	written, err := io.Copy(file, result.Body)
	if err != nil {
		return fmt.Errorf("erro ao escrever arquivo: %w", err)
	}

	log.Printf("[S3] Download concluído: %d bytes", written)
	return nil
}

func (s *S3Client) Upload(ctx context.Context, localPath string, s3Path string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("erro ao abrir arquivo %s: %w", localPath, err)
	}
	defer file.Close()

	contentType := getContentType(localPath)

	log.Printf("[S3] Enviando %s -> s3://%s/%s (Content-Type: %s)", localPath, s.bucket, s3Path, contentType)

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(s3Path),
		Body:        file,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("erro ao enviar para S3: %w", err)
	}

	return nil
}

func (s *S3Client) UploadDirectory(ctx context.Context, localDir string, s3Prefix string) error {
	return filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}

		s3Key := s3Prefix + "/" + strings.ReplaceAll(relPath, string(os.PathSeparator), "/")
		return s.Upload(ctx, path, s3Key)
	})
}

func getContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/MP2T"
	case ".mp4":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}
