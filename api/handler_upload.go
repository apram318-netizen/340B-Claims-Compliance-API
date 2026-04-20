package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"claims-system/internal/database"

	"github.com/xuri/excelize/v2"
	"github.com/jackc/pgx/v5/pgtype"
	amqp091 "github.com/rabbitmq/amqp091-go"
)

const maxUploadSize = 10 * 1024 * 1024 // 10 MB

var (
	errUnsupportedUploadType = errors.New("only .csv and .xlsx files are accepted")
	errInvalidHeader         = errors.New("file is missing required columns: ndc, pharmacy_npi, service_date, quantity")
	errInvalidFileFormat     = errors.New("failed to parse upload file")
)

// sanitizeCSVCell prefixes cells that start with formula-injection characters
// (=, +, -, @, \t, \r) with a single quote so spreadsheet applications treat
// the value as plain text rather than a formula.
func sanitizeCSVCell(s string) string {
	if len(s) == 0 {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// expectedCSVHeaders are the columns the upload CSV must contain (order-independent).
var expectedCSVHeaders = []string{
	"ndc", "pharmacy_npi", "service_date", "quantity",
}

// POST /v1/batches/upload
//
// Accepts multipart/form-data with:
//   - file  : CSV or XLSX file
//
// Optional columns: hashed_rx_key, payer_type
func (apiCfg *apiConfig) handlerUploadBatch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	userID, err := getUserId(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		respondWithValidationIssues(w, r, "invalid upload payload", []ValidationIssue{
			{Field: "file", Message: "must be multipart/form-data and within size limits"},
		})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondWithValidationIssues(w, r, "invalid upload payload", []ValidationIssue{
			{Field: "file", Message: "is required"},
		})
		return
	}
	defer file.Close()

	dataRows, err := parseUploadRows(file, header.Filename)
	if err != nil {
		respondWithValidationIssues(w, r, err.Error(), []ValidationIssue{
			{Field: "file", Message: err.Error()},
		})
		return
	}

	if len(dataRows) == 0 {
		respondWithValidationIssues(w, r, "upload contains no data rows", []ValidationIssue{
			{Field: "file", Message: "contains no data rows"},
		})
		return
	}

	// Create the batch record.
	batch, err := apiCfg.DB.CreateUploadBatches(r.Context(), database.CreateUploadBatchesParams{
		OrgID:      user.OrgID,
		UploadedBy: userID,
		FileName:   header.Filename,
		RowCount:   int32(len(dataRows)),
		Status:     "uploaded",
	})
	if err != nil {
		slog.Error("create upload batch failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to create batch")
		return
	}

	// Persist each row as JSON (matching the existing pipeline format).
	for i, dr := range dataRows {
		rawJSON, _ := json.Marshal(dr)
		if _, err := apiCfg.DB.CreateUploadRow(r.Context(), database.CreateUploadRowParams{
			BatchID:   batch.ID,
			RowNumber: int32(i + 1),
			Data:      rawJSON,
		}); err != nil {
			slog.Error("save upload row failed", "row", i+1, "error", err)
			respondWithError(w, http.StatusInternalServerError, "failed to save row")
			return
		}
	}

	// Audit event.
	if _, err := apiCfg.DB.CreateAuditEvent(r.Context(), database.CreateAuditEventParams{
		EventType:  "batch_uploaded",
		EntityType: "batch",
		EntityID:   batch.ID.String(),
		ActorID:    pgtype.UUID{Bytes: userID, Valid: true},
		Payload:    nil,
	}); err != nil {
		slog.Error("audit event failed", "batch_id", batch.ID, "error", err)
	}

	// Enqueue for validation.
	if err := apiCfg.Queue.PublishWithContext(r.Context(), "", "batch_validation", false, false,
		amqp091.Publishing{
			ContentType:  "text/plain",
			Body:         []byte(batch.ID.String()),
			DeliveryMode: amqp091.Persistent,
		},
	); err != nil {
		slog.Error("queue publish failed", "batch_id", batch.ID, "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to queue batch for processing")
		return
	}

	respondWithJSON(w, http.StatusCreated, databaseBatchToBatch(batch))
}

// indexHeaders returns a map of column name → CSV column index, plus a list
// of any required columns that are absent from the header row.
func indexHeaders(headers []string) (map[string]int, []string) {
	index := make(map[string]int, len(headers))
	for i, h := range headers {
		index[strings.ToLower(strings.TrimSpace(h))] = i
	}

	var missing []string
	for _, req := range expectedCSVHeaders {
		if _, ok := index[req]; !ok {
			missing = append(missing, req)
		}
	}
	return index, missing
}

func parseUploadRows(file io.Reader, filename string) ([]map[string]string, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".csv":
		return parseCSVRows(file)
	case ".xlsx":
		return parseExcelRows(file)
	default:
		return nil, errUnsupportedUploadType
	}
}

func parseCSVRows(file io.Reader) ([]map[string]string, error) {
	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true

	headers, err := reader.Read()
	if err != nil {
		return nil, errInvalidFileFormat
	}

	var rows [][]string
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errInvalidFileFormat
		}
		rows = append(rows, record)
	}
	return rowsToMaps(headers, rows)
}

func parseExcelRows(file io.Reader) ([]map[string]string, error) {
	xlsx, err := excelize.OpenReader(file)
	if err != nil {
		return nil, errInvalidFileFormat
	}
	defer xlsx.Close()

	sheets := xlsx.GetSheetList()
	if len(sheets) == 0 {
		return nil, errInvalidFileFormat
	}

	rows, err := xlsx.GetRows(sheets[0])
	if err != nil || len(rows) == 0 {
		return nil, errInvalidFileFormat
	}
	return rowsToMaps(rows[0], rows[1:])
}

func rowsToMaps(headers []string, rows [][]string) ([]map[string]string, error) {
	headerIndex, missing := indexHeaders(headers)
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", errInvalidHeader, strings.Join(missing, ", "))
	}

	var dataRows []map[string]string
	for _, record := range rows {
		// Skip entirely blank rows.
		isBlank := true
		for _, cell := range record {
			if strings.TrimSpace(cell) != "" {
				isBlank = false
				break
			}
		}
		if isBlank {
			continue
		}

		r := make(map[string]string, len(headers))
		for col, idx := range headerIndex {
			if idx < len(record) {
				r[col] = sanitizeCSVCell(strings.TrimSpace(record[idx]))
			} else {
				r[col] = ""
			}
		}
		dataRows = append(dataRows, r)
	}
	return dataRows, nil
}
