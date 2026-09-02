package main

import (
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"chirepk/backend/internal/application"
	"chirepk/backend/internal/store"
	httpapi "chirepk/backend/internal/transport/http"
	"chirepk/frontend"
)

func main() {
	defaultAddress := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		defaultAddress = ":" + port
	}
	address := flag.String("addr", defaultAddress, "HTTP listen address")
	flag.Parse()

	storage := store.NewMemoryStore()
	service := application.NewService(storage)
	api := httpapi.NewAPI(service)
	auth := httpapi.NewAuthenticator(httpapi.AuthConfig{
		Local: httpapi.LocalAuthConfig{
			Username: os.Getenv("CHIREPK_ADMIN_USERNAME"),
			Password: os.Getenv("CHIREPK_ADMIN_PASSWORD"),
		},
		Feishu: httpapi.FeishuAuthConfig{
			AppID:       os.Getenv("FEISHU_APP_ID"),
			AppSecret:   os.Getenv("FEISHU_APP_SECRET"),
			RedirectURL: os.Getenv("FEISHU_REDIRECT_URL"),
		},
		SecureCookies: strings.EqualFold(strings.TrimSpace(os.Getenv("CHIREPK_SECURE_COOKIES")), "true"),
	})
	mux := http.NewServeMux()
	auth.Register(mux)
	apiMux := http.NewServeMux()
	api.Register(apiMux)
	mux.Handle("/api/", auth.Require(apiMux))
	assets, err := fs.Sub(frontend.Files, "static")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(assets)))

	server := &http.Server{
		Addr:              *address,
		Handler:           httpapi.RequestLogger(log.Default(), mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("chirepk is running at http://localhost%s", *address)
	log.Fatal(server.ListenAndServe())
}
