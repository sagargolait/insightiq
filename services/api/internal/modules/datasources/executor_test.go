package datasources

import (
	"errors"
	"testing"
)

func TestValidateQueryAllowsReadOnly(t *testing.T) {
	ok := []string{
		"SELECT 1",
		"select * from orders",
		"SELECT count(*) FROM sales WHERE region = 'us'",
		"  select a, b from t  ",
		"WITH recent AS (SELECT * FROM orders WHERE created_at > now() - interval '1 day') SELECT count(*) FROM recent",
		"SELECT 1;",                      // single trailing semicolon tolerated
		"SELECT 1 -- a trailing comment", // comment stripped
		"SELECT sum(amount), avg(amount), min(amount), max(amount) FROM sales",
	}
	for _, q := range ok {
		if _, err := ValidateQuery(q); err != nil {
			t.Errorf("ValidateQuery(%q) = %v, want nil", q, err)
		}
	}
}

func TestValidateQueryRejects(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want error
	}{
		{"empty", "", ErrEmptyQuery},
		{"whitespace", "   \n\t ", ErrEmptyQuery},
		{"only comment", "-- just a comment", ErrEmptyQuery},
		{"insert", "INSERT INTO t VALUES (1)", ErrNotReadOnly},
		{"update", "UPDATE t SET x = 1", ErrNotReadOnly},
		{"delete", "DELETE FROM t", ErrNotReadOnly},
		{"drop", "DROP TABLE t", ErrNotReadOnly},
		{"truncate", "TRUNCATE t", ErrNotReadOnly},
		{"alter", "ALTER TABLE t ADD COLUMN c int", ErrNotReadOnly},
		{"create", "CREATE TABLE t (id int)", ErrNotReadOnly},
		{"grant", "GRANT ALL ON t TO public", ErrNotReadOnly},
		{"copy", "COPY t TO '/tmp/x'", ErrNotReadOnly},
		{"set", "SET statement_timeout = 0", ErrNotReadOnly},
		{"call", "CALL do_thing()", ErrNotReadOnly},
		{"do", "DO $$ BEGIN END $$", ErrNotReadOnly},
		{"explain", "EXPLAIN SELECT 1", ErrNotReadOnly},
		{"stacked write", "SELECT 1; DROP TABLE t", ErrMultipleStatement},
		{"stacked select", "SELECT 1; SELECT 2", ErrMultipleStatement},
		{"trailing semicolon then comment", "SELECT 1; -- x", nil}, // comment stripped, lone trailing ';' tolerated
		{"two statements", "SELECT 1; SELECT 2 -- x", ErrMultipleStatement},
		{"cte write delete", "WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d", ErrNotReadOnly},
		{"cte write update", "WITH u AS (UPDATE t SET x=1 RETURNING *) SELECT * FROM u", ErrNotReadOnly},
		{"cte write insert", "WITH i AS (INSERT INTO t VALUES (1) RETURNING *) SELECT * FROM i", ErrNotReadOnly},
		{"pg_catalog", "SELECT * FROM pg_catalog.pg_roles", ErrForbiddenTable},
		{"information_schema", "SELECT * FROM information_schema.tables", ErrForbiddenTable},
		{"pg_toast", "SELECT * FROM pg_toast.whatever", ErrForbiddenTable},
		{"comment hides drop", "SELECT 1 /* */; DROP TABLE t", ErrMultipleStatement},
		{"block comment smuggle", "SELECT 1 /* ; DROP TABLE t */", nil}, // comment stripped → valid single SELECT
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ValidateQuery(c.sql)
			if c.want == nil {
				if err != nil {
					t.Errorf("ValidateQuery(%q) = %v, want nil", c.sql, err)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Errorf("ValidateQuery(%q) = %v, want %v", c.sql, err, c.want)
			}
		})
	}
}

func TestStripComments(t *testing.T) {
	got := stripComments("SELECT 1 -- drop\nFROM t /* DELETE */ WHERE x=1")
	// Comments gone; the DELETE inside the block comment must not survive as a token.
	if containsToken(lowerStr(got), "delete") || containsToken(lowerStr(got), "drop") {
		t.Errorf("stripComments left a keyword in: %q", got)
	}
}

func lowerStr(s string) string {
	b := []byte(s)
	for i := range b {
		b[i] = lower(b[i])
	}
	return string(b)
}
