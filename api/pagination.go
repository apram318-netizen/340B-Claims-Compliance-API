package main

import (
	"net/http"
	"strconv"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 500
)

func parsePagination(r *http.Request) (limit, offset int, ok bool) {
	limit = defaultPageLimit
	offset = 0

	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			return 0, 0, false
		}
		if v > maxPageLimit {
			v = maxPageLimit
		}
		limit = v
	}

	if raw := r.URL.Query().Get("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return 0, 0, false
		}
		offset = v
	}
	return limit, offset, true
}

func applyPaginationHeaders(w http.ResponseWriter, total int64, limit, offset int) {
	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	w.Header().Set("X-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Offset", strconv.Itoa(offset))
}

func paginate[T any](in []T, limit, offset int) []T {
	if offset >= len(in) {
		return []T{}
	}
	end := offset + limit
	if end > len(in) {
		end = len(in)
	}
	return in[offset:end]
}
