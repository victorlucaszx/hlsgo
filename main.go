package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("[MAIN] Iniciando serviço HLS Converter")

	// Load .env file (ignore error if not present)
	if err := godotenv.Load(); err != nil {
		log.Println("[MAIN] Arquivo .env não encontrado, usando variáveis de ambiente do sistema")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8001"
	}

	queue := NewJobQueue()
	handler := NewHandler(queue)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/hls/convert", handler.HandleConvert)
	mux.HandleFunc("/api/hls/health", handler.HandleHealth)
	mux.HandleFunc("/api/hls/", func(w http.ResponseWriter, r *http.Request) {
		// Route DELETE /api/hls/{conversion_id}
		if r.Method == http.MethodDelete {
			path := strings.TrimPrefix(r.URL.Path, "/api/hls/")
			if path != "" && path != "convert" && path != "health" {
				handler.HandleCancel(w, r)
				return
			}
		}
		http.NotFound(w, r)
	})

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigChan
		log.Printf("[MAIN] Sinal recebido: %v. Encerrando...", sig)
		queue.Shutdown()
		os.Exit(0)
	}()

	addr := ":" + port
	log.Printf("[MAIN] Servidor HTTP escutando em %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[MAIN] Erro ao iniciar servidor: %v", err)
	}
}
