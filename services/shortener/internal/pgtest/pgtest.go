// Package pgtest gives each test its own throwaway Postgres database.
package pgtest

import (
	"crypto/rand"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	"github.com/stretchr/testify/require"
)

// EnvURL names the variable that points tests at a Postgres server. `make test` sets it.
const EnvURL = "SHORTENER_TEST_DATABASE_URL"

// New creates a fresh database on the server at EnvURL and drops it when the
// test ends. The test fails when EnvURL is unset, so a run without a database
// never reports a pass it did not earn.
func New(t *testing.T) *sql.DB {
	t.Helper()

	base := os.Getenv(EnvURL)
	if base == "" {
		t.Fatalf("%s unset; run `make test`, or `make test-pg` and export it", EnvURL)
	}
	admin, err := sql.Open("pgx", base)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })

	name := "t_" + strings.ToLower(rand.Text())
	_, err = admin.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)

	u, err := url.Parse(base)
	require.NoError(t, err)
	u.Path = "/" + name
	db, err := sql.Open("pgx", u.String())
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec(`SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE database = $1`, name)
		_, _ = admin.Exec("DROP DATABASE " + name + " WITH (FORCE)")
	})
	return db
}
