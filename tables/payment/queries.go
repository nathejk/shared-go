package payment

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

// Aliases of the module-wide sentinels, kept so callers can keep writing
// payment.ErrRecordNotFound. errors.Is against tables.ErrRecordNotFound works
// either way.
var (
	ErrRecordNotFound = tables.ErrRecordNotFound
	ErrEditConflict   = tables.ErrEditConflict
)

type Metadata struct{}
type Payment struct {
	Reference       string              `json:"reference"`
	Year            string              `json:"year"`
	ReceiptEmail    types.EmailAddress  `json:"receiptEmail"`
	ReturnUrl       string              `json:"returnUrl"`
	Currency        types.Currency      `json:"currency"`
	Amount          int                 `json:"amount"`
	Method          string              `json:"method"`
	Status          types.PaymentStatus `json:"status"`
	CreatedAt       string              `json:"createdAt"`
	ChangedAt       string              `json:"changedAt"`
	OrderForeignKey string              `json:"orderForeignKey"`
	OrderType       string              `json:"orderType"`
}

type Query struct {
	DB cqrs.Reader
}

func (q Query) GetAll(teamID types.TeamID) ([]Payment, Metadata, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Returns every payment routed to this team, whether directly
	// (legacy: payment.orderForeignKey = teamId) or through the new order
	// entity (payment.orderForeignKey = orderId, orders.ownerId = teamId).
	// FLOOR(amount/100) keeps the legacy contract: callers see DKK.
	query := `SELECT p.reference, p.receiptEmail, p.returnUrl, p.year, p.currency, FLOOR(p.amount/100), p.method, p.status, p.createdAt, p.changedAt, p.orderForeignKey, p.orderType
		FROM payment p
		LEFT JOIN orders o ON o.orderId = p.orderForeignKey
		WHERE p.orderForeignKey = ? OR o.ownerId = ?
		ORDER BY p.createdAt ASC`
	args := []any{teamID, teamID}
	rows, err := q.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, Metadata{}, err
	}
	defer rows.Close()

	//	totalRecords := 0
	var payments []Payment
	for rows.Next() {
		var p Payment
		err := rows.Scan(&p.Reference, &p.ReceiptEmail, &p.ReturnUrl, &p.Year, &p.Currency, &p.Amount, &p.Method, &p.Status, &p.CreatedAt, &p.ChangedAt, &p.OrderForeignKey, &p.OrderType)
		if err != nil {
			return nil, Metadata{}, err
		}
		payments = append(payments, p)
	}

	// When the rows.Next() loop has finished, call rows.Err() to retrieve any error
	// that was encountered during the iteration.
	if err = rows.Err(); err != nil {
		return nil, Metadata{}, err
	}
	metadata := Metadata{} //calculateMetadata(filters.Year, totalRecords, filters.Page, filters.PageSize)

	return payments, metadata, nil
	/*
		_, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		years := []*Year{
			&Year{Slug: "2023", Name: "Nathejk 2024", Theme: "Ex Nihilo", CityDeparture: "Kalundborg", CityDestination: "Stenlille"},
			&Year{Slug: "2022", Name: "Nathejk 2022", Theme: "Ufomania", CityDeparture: "Faxe", CityDestination: "Ringsted"},
			&Year{Slug: "2021", Name: "Nathejk 2021", Theme: "Kong Etruds Sværd", CityDeparture: "Helsingør", CityDestination: "Hillerød"},
		}
		return years, Metadata{}, nil
	*/
}

func (q Query) GetByReference(ref string) (*Payment, error) {
	if len(ref) == 0 {
		return nil, ErrRecordNotFound
	}

	query := `SELECT receiptEmail, returnUrl, year, currency, FLOOR(amount/100), method, status, createdAt, changedAt, orderForeignKey, orderType
		FROM payment
		WHERE reference = ?`
	var p Payment
	err := q.DB.QueryRow(query, ref).Scan(
		&p.ReceiptEmail,
		&p.ReturnUrl,
		&p.Year,
		&p.Currency,
		&p.Amount,
		&p.Method,
		&p.Status,
		&p.CreatedAt,
		&p.ChangedAt,
		&p.OrderForeignKey,
		&p.OrderType,
	)
	p.Reference = ref
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrRecordNotFound
		default:
			return nil, err
		}
	}
	return &p, nil
}

func (q Query) AmountDueByTeamID(teamID types.TeamID) {
}

func (q Query) AmountPaidByTeamID(teamID types.TeamID) int {
	// Sum of all reserved/received payments routed to this team, whether
	// directly (legacy: payment.orderForeignKey = teamId) or through the
	// new order entity (payment.orderForeignKey = orderId, orders.ownerId
	// = teamId). Returns DKK (the amount column is in øre; FLOOR divides
	// the total back to whole DKK to match the legacy contract).
	query := `SELECT FLOOR(COALESCE(SUM(p.amount), 0) / 100)
		FROM payment p
		LEFT JOIN orders o ON o.orderId = p.orderForeignKey
		WHERE p.status IN (?, ?)
		  AND (p.orderForeignKey = ? OR o.ownerId = ?)`
	var paidAmount int
	if err := q.DB.QueryRow(query, types.PaymentStatusReserved, types.PaymentStatusReceived, teamID, teamID).Scan(&paidAmount); err != nil {
		return 0
	}
	return paidAmount
}

func (q Query) ConfirmBySecret(secret string) (types.TeamID, error) {
	//ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	//defer cancel()

	query := "UPDATE signup s JOIN confirm c ON s.teamId = c.teamId SET s.email = c.emailPending WHERE secret = ?"
	//result, err := m.DB.Exec(query, secret)
	_, err := q.DB.Exec(query, secret)
	if err != nil {
		return "", err
	}
	/*
		rowCount, err := result.RowsAffected()
		if err != nil {
			return "", err
		}
		if rowCount != 1 {
			return "", fmt.Errorf("e-mail not found")
		}
	*/
	var teamID types.TeamID
	err = q.DB.QueryRow(`SELECT teamId FROM confirm WHERE secret = ?`, secret).Scan(&teamID)
	if err != nil {
		return "", err
	}

	return teamID, nil
}
