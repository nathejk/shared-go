package payment

import (
	"regexp"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jrgensen/cqrs/cqrstest"
)

// The failure this test exists to prevent is silent and only happens in
// production: CREATE TABLE IF NOT EXISTS is a no-op wherever a payment table
// already exists, so a column declared only in table.sql appears in a fresh
// database and is missing from every real one — and then every projection
// statement is dead-lettered on an unknown column.
//
// So the interesting case is a *pre-existing* table: the schema statement changes
// nothing, and the ALTERs are the only thing that adds the columns.
func TestNewMigratesAPreExistingPaymentTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	// A database that has the payment table but none of the provenance schema:
	// every existence check comes back 0, as it would on a real upgrade.
	missing := sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0)
	mock.ExpectQuery("INFORMATION_SCHEMA.COLUMNS").WillReturnRows(missing)
	mock.ExpectQuery("INFORMATION_SCHEMA.COLUMNS").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
	mock.ExpectQuery("INFORMATION_SCHEMA.STATISTICS").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))

	w := &cqrstest.Writer{}
	New(&cqrstest.Publisher{}, w, db, "2026")

	joined := strings.Join(w.Statements, "\n")
	for _, want := range []string{
		"ADD COLUMN sourceReference",
		"ADD COLUMN source JSON",
		"ADD INDEX idx_payment_source",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q; an existing database would never get it:\n%s", want, joined)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("the existence checks must go through the Reader: %v", err)
	}
}

// Idempotent: a database that already has the columns must not be altered again on
// every start.
func TestNewSkipsMigrationsThatAlreadyRan(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	present := func() *sqlmock.Rows { return sqlmock.NewRows([]string{"n"}).AddRow(1) }
	mock.ExpectQuery("INFORMATION_SCHEMA.COLUMNS").WillReturnRows(present())
	mock.ExpectQuery("INFORMATION_SCHEMA.COLUMNS").WillReturnRows(present())
	mock.ExpectQuery("INFORMATION_SCHEMA.STATISTICS").WillReturnRows(present())

	w := &cqrstest.Writer{}
	New(&cqrstest.Publisher{}, w, db, "2026")

	for _, s := range w.Statements {
		// addOperationsColumn is the older hand-rolled idiom and is issued
		// unconditionally, relying on MariaDB's IF NOT EXISTS. It is exempt
		// precisely because that is the pattern cqrs.EnsureColumn replaces.
		if strings.Contains(s, "IF NOT EXISTS operations") {
			continue
		}
		if strings.Contains(s, "ADD COLUMN") || strings.Contains(s, "ADD INDEX") {
			t.Errorf("nothing should be altered when it already exists, got %q", s)
		}
	}
}

// The schema file must declare the same columns, or a freshly created database
// would depend on the migrations to be complete — and they only run once.
func TestSchemaDeclaresProvenanceColumns(t *testing.T) {
	for _, want := range []string{"sourceReference", "source JSON", "idx_payment_source"} {
		if !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(tableSchema) {
			t.Errorf("table.sql does not declare %q", want)
		}
	}
}
