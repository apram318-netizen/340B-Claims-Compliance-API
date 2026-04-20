package main

import (
	"context"
	"net/http"
	"time"
)

func (apiCfg *apiConfig) handlerHealth(w http.ResponseWriter, r *http.Request) {
	if apiCfg.Pool == nil {
		respondWithJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status":   "degraded",
			"database": "unconfigured",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	type healthResponse struct {
		Status   string `json:"status"`
		Database string `json:"database"`
	}

	if err := apiCfg.Pool.Ping(ctx); err != nil {
		respondWithJSON(w, http.StatusServiceUnavailable, healthResponse{
			Status:   "degraded",
			Database: "unreachable",
		})
		return
	}

	respondWithJSON(w, http.StatusOK, healthResponse{
		Status:   "ok",
		Database: "connected",
	})
}
