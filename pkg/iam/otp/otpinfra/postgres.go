package otpinfra

import (
	"context"
	"database/sql"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam/otp"
	"github.com/jmoiron/sqlx"
)

type PostgresOTPRepository struct {
	db *sqlx.DB
}

func NewPostgresOTPRepository(db *sqlx.DB) *PostgresOTPRepository {
	return &PostgresOTPRepository{db: db}
}

// Create inserts a new OTP into the database
func (r *PostgresOTPRepository) Create(ctx context.Context, o *otp.OTP) error {
	query := `
        INSERT INTO otps (id, contact, code, purpose, expires_at, attempts, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `

	_, err := r.db.ExecContext(
		ctx,
		query,
		o.ID,
		o.Contact,
		o.Code,
		string(o.Purpose),
		o.ExpiresAt,
		o.Attempts,
		o.CreatedAt,
		time.Now(),
	)

	if err != nil {
		return errx.Wrap(err, "failed to create OTP", errx.TypeInternal)
	}

	return nil
}

// GetByContactAndCode retrieves an OTP by contact and code
func (r *PostgresOTPRepository) GetByContactAndCode(ctx context.Context, contact string, code string) (*otp.OTP, error) {
	query := `
        SELECT id, contact, code, purpose, expires_at, verified_at, attempts, created_at
        FROM otps
        WHERE contact = $1 AND code = $2
        ORDER BY created_at DESC
        LIMIT 1
    `

	var o otp.OTP
	var verifiedAt sql.NullTime
	var purposeStr string

	err := r.db.QueryRowContext(ctx, query, contact, code).Scan(
		&o.ID,
		&o.Contact,
		&o.Code,
		&purposeStr,
		&o.ExpiresAt,
		&verifiedAt,
		&o.Attempts,
		&o.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, otp.ErrInvalidOTP()
	}
	if err != nil {
		return nil, errx.Wrap(err, "failed to get OTP", errx.TypeInternal)
	}

	o.Purpose = otp.OTPPurpose(purposeStr)
	if verifiedAt.Valid {
		o.VerifiedAt = &verifiedAt.Time
	}

	return &o, nil
}

// GetLatestByContact retrieves the most recent OTP for a contact and purpose
func (r *PostgresOTPRepository) GetLatestByContact(ctx context.Context, contact string, purpose otp.OTPPurpose) (*otp.OTP, error) {
	query := `
        SELECT id, contact, code, purpose, expires_at, verified_at, attempts, created_at
        FROM otps
        WHERE contact = $1 AND purpose = $2
        ORDER BY created_at DESC
        LIMIT 1
    `

	var o otp.OTP
	var verifiedAt sql.NullTime
	var purposeStr string

	err := r.db.QueryRowContext(ctx, query, contact, string(purpose)).Scan(
		&o.ID,
		&o.Contact,
		&o.Code,
		&purposeStr,
		&o.ExpiresAt,
		&verifiedAt,
		&o.Attempts,
		&o.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No OTP found is not an error in this case
	}
	if err != nil {
		return nil, errx.Wrap(err, "failed to get latest OTP", errx.TypeInternal)
	}

	o.Purpose = otp.OTPPurpose(purposeStr)
	if verifiedAt.Valid {
		o.VerifiedAt = &verifiedAt.Time
	}

	return &o, nil
}

// Update updates an existing OTP
func (r *PostgresOTPRepository) Update(ctx context.Context, o *otp.OTP) error {
	query := `
        UPDATE otps
        SET verified_at = $1, attempts = $2, updated_at = $3
        WHERE id = $4
    `

	var verifiedAt interface{}
	if o.VerifiedAt != nil {
		verifiedAt = *o.VerifiedAt
	}

	result, err := r.db.ExecContext(
		ctx,
		query,
		verifiedAt,
		o.Attempts,
		time.Now(),
		o.ID,
	)

	if err != nil {
		return errx.Wrap(err, "failed to update OTP", errx.TypeInternal)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rows == 0 {
		return errx.New("OTP not found", errx.TypeNotFound)
	}

	return nil
}

// DeleteExpired removes all expired OTPs from the database
func (r *PostgresOTPRepository) DeleteExpired(ctx context.Context) error {
	query := `
        DELETE FROM otps
        WHERE expires_at < $1
    `

	_, err := r.db.ExecContext(ctx, query, time.Now())
	if err != nil {
		return errx.Wrap(err, "failed to delete expired OTPs", errx.TypeInternal)
	}

	return nil
}

