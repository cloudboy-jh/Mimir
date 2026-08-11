package demo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/cloudboy-jh/mimir/internal/demoassets"
)

type Server struct {
	listener net.Listener
	http     *http.Server
	url      string
}

func Start() (*Server, error) {
	assets, err := fs.Sub(demoassets.Files, "static")
	if err != nil {
		return nil, fmt.Errorf("loading embedded demo: %w", err)
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, fmt.Errorf("loading embedded demo index: %w", err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting demo listener: %w", err)
	}
	return &Server{
		listener: listener,
		http:     &http.Server{Handler: handler(assets, index), ReadHeaderTimeout: 5 * time.Second},
		url:      "http://" + listener.Addr().String() + "/",
	}, nil
}

func (s *Server) URL() string { return s.url }

func (s *Server) Address() net.Addr { return s.listener.Addr() }

func (s *Server) Serve(ctx context.Context) error {
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = s.http.Shutdown(shutdownCtx)
		case <-stopped:
		}
	}()
	err := s.http.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) && ctx.Err() != nil {
		return nil
	}
	return err
}

func (s *Server) Close() error {
	httpErr := s.http.Close()
	listenerErr := s.listener.Close()
	if errors.Is(httpErr, http.ErrServerClosed) {
		httpErr = nil
	}
	if errors.Is(listenerErr, net.ErrClosed) {
		listenerErr = nil
	}
	return errors.Join(httpErr, listenerErr)
}

func handler(assets fs.FS, index []byte) http.Handler {
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			response.Header().Set("Allow", "GET, HEAD")
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := strings.TrimPrefix(path.Clean("/"+request.URL.Path), "/")
		if clean == "." {
			clean = ""
		}
		for _, prefix := range []string{"dashboard/api", "dashboard/auth", "dashboard/log-objects"} {
			if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
				http.NotFound(response, request)
				return
			}
		}
		if clean != "" {
			if info, err := fs.Stat(assets, clean); err == nil && !info.IsDir() {
				if strings.HasPrefix(clean, "assets/") {
					response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					response.Header().Set("Cache-Control", "no-store")
				}
				files.ServeHTTP(response, request)
				return
			}
			if clean == "assets" || strings.HasPrefix(clean, "assets/") {
				http.NotFound(response, request)
				return
			}
		}
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		if request.Method == http.MethodGet {
			_, _ = response.Write(index)
		}
	})
}
