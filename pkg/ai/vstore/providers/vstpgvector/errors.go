package vstpgvector

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Abraxas-365/manifesto/pkg/errx"
)

var (
	// Error registry for pgvector provider
	errorRegistry = errx.NewRegistry("PGVECTOR")

	// Database Errors
	ErrDatabaseConnection = errorRegistry.Register(
		"DB_CONNECTION_FAILED",
		errx.TypeExternal,
		http.StatusServiceUnavailable,
		"Failed to connect to PostgreSQL database",
	)

	ErrDatabaseQuery = errorRegistry.Register(
		"DB_QUERY_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Database query execution failed",
	)

	ErrTransactionFailed = errorRegistry.Register(
		"DB_TRANSACTION_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Database transaction failed",
	)

	ErrExtensionNotInstalled = errorRegistry.Register(
		"EXTENSION_NOT_INSTALLED",
		errx.TypeValidation,
		http.StatusPreconditionFailed,
		"pgvector extension is not installed",
	)

	// Input Errors
	ErrInvalidInput = errorRegistry.Register(
		"INVALID_INPUT",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Invalid input parameters",
	)

	ErrInvalidVectorDimension = errorRegistry.Register(
		"INVALID_VECTOR_DIMENSION",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Vector dimension mismatch",
	)

	ErrEmptyVectorID = errorRegistry.Register(
		"EMPTY_VECTOR_ID",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Vector ID cannot be empty",
	)

	ErrInvalidNamespace = errorRegistry.Register(
		"INVALID_NAMESPACE",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Invalid namespace name",
	)

	// Table/Index Errors
	ErrTableNotFound = errorRegistry.Register(
		"TABLE_NOT_FOUND",
		errx.TypeNotFound,
		http.StatusNotFound,
		"Vector table does not exist",
	)

	ErrTableAlreadyExists = errorRegistry.Register(
		"TABLE_ALREADY_EXISTS",
		errx.TypeValidation,
		http.StatusConflict,
		"Vector table already exists",
	)

	ErrIndexCreationFailed = errorRegistry.Register(
		"INDEX_CREATION_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Failed to create vector index",
	)

	ErrIndexNotFound = errorRegistry.Register(
		"INDEX_NOT_FOUND",
		errx.TypeNotFound,
		http.StatusNotFound,
		"Vector index does not exist",
	)

	// Operation Errors
	ErrUpsertFailed = errorRegistry.Register(
		"UPSERT_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Failed to upsert vectors",
	)

	ErrQueryFailed = errorRegistry.Register(
		"QUERY_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Failed to query vectors",
	)

	ErrDeleteFailed = errorRegistry.Register(
		"DELETE_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Failed to delete vectors",
	)

	ErrFetchFailed = errorRegistry.Register(
		"FETCH_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Failed to fetch vectors",
	)

	// Configuration Errors
	ErrMissingConfig = errorRegistry.Register(
		"MISSING_CONFIG",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Missing required configuration",
	)

	ErrInvalidConfig = errorRegistry.Register(
		"INVALID_CONFIG",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Invalid configuration",
	)

	ErrInvalidMetric = errorRegistry.Register(
		"INVALID_METRIC",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Invalid distance metric",
	)

	// Feature Support Errors
	ErrFeatureNotSupported = errorRegistry.Register(
		"FEATURE_NOT_SUPPORTED",
		errx.TypeValidation,
		http.StatusNotImplemented,
		"Feature not supported by pgvector",
	)

	ErrSparseVectorsNotSupported = errorRegistry.Register(
		"SPARSE_VECTORS_NOT_SUPPORTED",
		errx.TypeValidation,
		http.StatusNotImplemented,
		"Sparse vectors are not supported by pgvector",
	)
)

// DatabaseError represents a database-specific error
type DatabaseError struct {
	Query      string
	Params     []any
	Message    string
	SQLState   string
	Constraint string
}

// Error implements the error interface
func (e *DatabaseError) Error() string {
	if e.SQLState != "" {
		return fmt.Sprintf("Database error (SQLState: %s): %s", e.SQLState, e.Message)
	}
	return fmt.Sprintf("Database error: %s", e.Message)
}

// ParseDatabaseError parses a database error into a custom error
func ParseDatabaseError(err error, query string, params ...any) *errx.Error {
	if err == nil {
		return nil
	}

	dbErr := &DatabaseError{
		Query:   query,
		Params:  params,
		Message: err.Error(),
	}

	// Try to extract PostgreSQL error details
	// This depends on your database driver (pgx, lib/pq, etc.)
	// Adjust according to your driver's error types

	var baseErr *errx.ErrorCode

	// Check for specific PostgreSQL error codes
	errMsg := err.Error()

	// Connection errors
	if contains(errMsg, "connection refused", "connection reset", "connection closed") {
		baseErr = ErrDatabaseConnection
	} else if contains(errMsg, "does not exist") {
		baseErr = ErrTableNotFound
	} else if contains(errMsg, "already exists") {
		baseErr = ErrTableAlreadyExists
	} else if contains(errMsg, "extension") {
		baseErr = ErrExtensionNotInstalled
	} else {
		baseErr = ErrDatabaseQuery
	}

	customErr := errorRegistry.NewWithCause(baseErr, err)
	customErr.WithDetail("query", query)

	if len(params) > 0 {
		// Convert params to JSON for better logging
		if paramsJSON, jsonErr := json.Marshal(params); jsonErr == nil {
			customErr.WithDetail("params", string(paramsJSON))
		}
	}

	if dbErr.SQLState != "" {
		customErr.WithDetail("sql_state", dbErr.SQLState)
	}

	return customErr
}

// WrapError wraps a standard error with appropriate pgvector error code
func WrapError(err error, code *errx.ErrorCode) *errx.Error {
	if err == nil {
		return nil
	}

	// Check if it's already a custom error
	var customErr *errx.Error
	if errx.As(err, &customErr) {
		return customErr
	}

	return errorRegistry.NewWithCause(code, err)
}

// Helper function to check if error message contains any of the strings
func contains(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}
