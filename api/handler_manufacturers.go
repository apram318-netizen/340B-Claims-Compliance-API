package main

import (
	"claims-system/internal/database"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (apiCfg *apiConfig) handlerCreateManufacturer(w http.ResponseWriter, r *http.Request) {
	type requestBody struct {
		Name        string `json:"name"`
		LabelerCode string `json:"labeler_code"`
	}

	var body requestBody
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if body.Name == "" || body.LabelerCode == "" {
		issues := make([]ValidationIssue, 0, 2)
		if body.Name == "" {
			issues = append(issues, ValidationIssue{Field: "name", Message: "is required"})
		}
		if body.LabelerCode == "" {
			issues = append(issues, ValidationIssue{Field: "labeler_code", Message: "is required"})
		}
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	m, err := apiCfg.DB.CreateManufacturer(r.Context(), database.CreateManufacturerParams{
		Name:        body.Name,
		LabelerCode: body.LabelerCode,
	})
	if err != nil {
		slog.Error("create manufacturer failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to create manufacturer")
		return
	}

	respondWithJSON(w, http.StatusCreated, m)
}

func (apiCfg *apiConfig) handlerGetManufacturer(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid manufacturer id")
		return
	}

	m, err := apiCfg.DB.GetManufacturerByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "manufacturer not found")
			return
		}
		slog.Error("get manufacturer failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondWithJSON(w, http.StatusOK, m)
}

func (apiCfg *apiConfig) handlerListManufacturers(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parsePagination(r)
	if !ok {
		respondWithValidationIssues(w, r, "invalid pagination params", []ValidationIssue{
			{Field: "limit/offset", Message: "must be positive integers; offset must be >= 0"},
		})
		return
	}

	total, err := apiCfg.DB.CountManufacturers(r.Context())
	if err != nil {
		slog.Error("count manufacturers failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	manufacturers, err := apiCfg.DB.ListManufacturersPaginated(r.Context(), database.ListManufacturersPaginatedParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		slog.Error("list manufacturers failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	applyPaginationHeaders(w, total, limit, offset)
	respondWithJSON(w, http.StatusOK, manufacturers)
}

func (apiCfg *apiConfig) handlerCreateManufacturerProduct(w http.ResponseWriter, r *http.Request) {
	manufacturerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithValidationIssues(w, r, "invalid path params", []ValidationIssue{
			{Field: "id", Message: "must be a valid manufacturer UUID"},
		})
		return
	}

	type requestBody struct {
		NDC         string `json:"ndc"`
		ProductName string `json:"product_name"`
	}

	var body requestBody
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if body.NDC == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "ndc", Message: "is required"},
		})
		return
	}

	product, err := apiCfg.DB.CreateManufacturerProduct(r.Context(), database.CreateManufacturerProductParams{
		ManufacturerID: manufacturerID,
		Ndc:            body.NDC,
		ProductName:    pgTextFrom(body.ProductName),
	})
	if err != nil {
		slog.Error("create product failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to create product")
		return
	}

	respondWithJSON(w, http.StatusCreated, product)
}

func (apiCfg *apiConfig) handlerListManufacturerProducts(w http.ResponseWriter, r *http.Request) {
	manufacturerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid manufacturer id")
		return
	}

	limit, offset, ok := parsePagination(r)
	if !ok {
		respondWithValidationIssues(w, r, "invalid pagination params", []ValidationIssue{
			{Field: "limit/offset", Message: "must be positive integers; offset must be >= 0"},
		})
		return
	}

	total, err := apiCfg.DB.CountProductsByManufacturer(r.Context(), manufacturerID)
	if err != nil {
		slog.Error("count products failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	products, err := apiCfg.DB.ListProductsByManufacturerPaginated(r.Context(), database.ListProductsByManufacturerPaginatedParams{
		ManufacturerID: manufacturerID,
		Limit:          int32(limit),
		Offset:         int32(offset),
	})
	if err != nil {
		slog.Error("list products failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	applyPaginationHeaders(w, total, limit, offset)
	respondWithJSON(w, http.StatusOK, products)
}
