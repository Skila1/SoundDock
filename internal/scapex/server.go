package scapex

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func Handler(svc *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeErr(w, 400, "query required")
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		hits, err := svc.Search(r.Context(), q, limit)
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"query": q, "results": hits})
	})
	mux.HandleFunc("POST /fetch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL      string   `json:"url"`
			VideoID  string   `json:"video_id"`
			URLs     []string `json:"urls"`
			VideoIDs []string `json:"video_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && r.Body != http.NoBody {
			writeErr(w, 400, "invalid json")
			return
		}
		refs := append([]string{}, body.URLs...)
		refs = append(refs, body.VideoIDs...)
		if body.URL != "" {
			refs = append(refs, body.URL)
		}
		if body.VideoID != "" {
			refs = append(refs, body.VideoID)
		}
		if len(refs) == 0 {
			writeErr(w, 400, "url or video_id required")
			return
		}
		var ids []string
		for _, ref := range refs {
			got, err := svc.Fetch(r.Context(), ref)
			if err != nil {
				writeErr(w, 502, err.Error())
				return
			}
			for _, id := range got {
				ids = append(ids, id.String())
			}
		}
		writeJSON(w, 200, map[string]any{"track_ids": ids})
	})
	return mux
}

func ListenAndServe(addr string, svc *Service) error {
	if addr == "" {
		addr = ":7788"
	}
	s := &http.Server{
		Addr:              addr,
		Handler:           Handler(svc),
		ReadHeaderTimeout: 8 * time.Second,
	}
	return s.ListenAndServe()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
