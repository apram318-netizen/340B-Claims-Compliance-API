package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func decodeJSONStrict(r *http.Request, dst interface{}) []ValidationIssue {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return jsonDecodeIssues(err)
	}

	var trailing interface{}
	if err := dec.Decode(&trailing); err != io.EOF {
		return []ValidationIssue{{Field: "body", Message: "must contain a single JSON object"}}
	}
	return nil
}

func jsonDecodeIssues(err error) []ValidationIssue {
	var syntaxErr *json.SyntaxError
	var unmarshalTypeErr *json.UnmarshalTypeError

	switch {
	case errors.Is(err, io.EOF):
		return []ValidationIssue{{Field: "body", Message: "request body is required"}}
	case errors.As(err, &syntaxErr):
		return []ValidationIssue{{Field: "body", Message: fmt.Sprintf("contains malformed JSON at character %d", syntaxErr.Offset)}}
	case errors.As(err, &unmarshalTypeErr):
		field := unmarshalTypeErr.Field
		if field == "" {
			field = "body"
		}
		return []ValidationIssue{{Field: field, Message: fmt.Sprintf("must be %s", unmarshalTypeErr.Type.String())}}
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		field := strings.TrimPrefix(err.Error(), "json: unknown field ")
		field = strings.Trim(field, "\"")
		return []ValidationIssue{{Field: field, Message: "is not allowed"}}
	default:
		return []ValidationIssue{{Field: "body", Message: "contains invalid JSON payload"}}
	}
}
