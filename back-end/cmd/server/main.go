package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/luhao/contextGraph/internal/app"
	"github.com/luhao/contextGraph/config"
	"github.com/luhao/contextGraph/internal/api"
)

func main() {
	cfg := config.Load()

	a, err := app.New(cfg)
	if err != nil {
		log.Fatal("app init failed: ", err)
	}
	defer func() {
		_ = a.Close(context.Background())
	}()

	r := api.NewRouter(a)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: r,
	}

	// 启动 HTTP
	go func() {
		log.Println("server listening on", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("listen error: ", err)
		}
	}()

	// 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = srv.Shutdown(ctx)
	_ = a.Close(ctx)
	log.Println("server shutdown ok")
}
