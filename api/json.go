package main

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	dat, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal response", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)
}

type errorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"request_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	respondWithErrorCode(w, nil, code, msg, "")
}

func respondWithRequestError(w http.ResponseWriter, r *http.Request, code int, msg string) {
	respondWithErrorCode(w, r, code, msg, "")
}

func respondWithErrorCode(w http.ResponseWriter, r *http.Request, code int, msg, explicitCode string) {
	requestID := ""
	traceID := ""
	if r != nil {
		requestID = middleware.GetReqID(r.Context())
		traceID = traceIDFromContext(r.Context())
	}
	errorCode := explicitCode
	if errorCode == "" {
		errorCode = statusToErrorCode(code)
	}
	if code >= 500 {
		slog.Error("error response", "status", code, "code", errorCode, "message", msg, "request_id", requestID, "trace_id", traceID)
	}
	respondWithJSON(w, code, errorResponse{
		Error:     msg,
		Code:      errorCode,
		RequestID: requestID,
		TraceID:   traceID,
	})
}

func statusToErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "BAD_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusConflict:
		return "CONFLICT"
	case http.StatusRequestEntityTooLarge:
		return "PAYLOAD_TOO_LARGE"
	case http.StatusUnsupportedMediaType:
		return "UNSUPPORTED_MEDIA_TYPE"
	case http.StatusTooManyRequests:
		return "RATE_LIMITED"
	case http.StatusServiceUnavailable:
		return "SERVICE_UNAVAILABLE"
	case http.StatusInternalServerError:
		return "INTERNAL_ERROR"
	default:
		return "API_ERROR"
	}
}

type ValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type validationErrorResponse struct {
	Error     string            `json:"error"`
	Code      string            `json:"code"`
	RequestID string            `json:"request_id,omitempty"`
	TraceID   string            `json:"trace_id,omitempty"`
	Details   []ValidationIssue `json:"details,omitempty"`
}

func respondWithValidationIssues(w http.ResponseWriter, r *http.Request, msg string, issues []ValidationIssue) {
	requestID := middleware.GetReqID(r.Context())
	respondWithJSON(w, http.StatusBadRequest, validationErrorResponse{
		Error:     msg,
		Code:      "VALIDATION_ERROR",
		RequestID: requestID,
		TraceID:   traceIDFromContext(r.Context()),
		Details:   issues,
	})
}
