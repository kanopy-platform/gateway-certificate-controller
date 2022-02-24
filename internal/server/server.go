package server

import (
	"fmt"
	"net/http"
)

func NewServer(port string) *http.Server {
	mux := http.NewServeMux()

	return &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: mux,
	}
}
