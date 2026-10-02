package httpapi

import (
	"context"
	"net/http"
	"time"
)

type ReadyApplication interface{ Ready(context.Context) error }

func ReadinessHandler(application ReadyApplication, client *http.Client, mainURL string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/ready" {
			http.NotFound(writer, request)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		check, err := http.NewRequestWithContext(ctx, http.MethodGet, mainURL, nil)
		if err == nil {
			var response *http.Response
			response, err = client.Do(check)
			if err == nil {
				_ = response.Body.Close()
				if response.StatusCode != http.StatusUnauthorized {
					err = context.Canceled
				}
			}
		}
		if err == nil {
			err = application.Ready(ctx)
		}
		if err != nil {
			http.Error(writer, "not ready", http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusOK)
	})
}
