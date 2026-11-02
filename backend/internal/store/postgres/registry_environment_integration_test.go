//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/model"
)

// This fixture refuses generic integration/owner databases. The matching
// harness creates only this dedicated role/database on an internal network.
func registryEnvironmentTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COOKER_REGISTRY_ENV_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("owned disposable registry-environment PostgreSQL required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "cooker-registry-env-pg" || u.Port() != "5432" || u.Path != "/cooker_registry_environment_test" || u.User == nil || u.User.Username() != "registry_env_fixture" || u.Query().Get("sslmode") != "disable" || len(u.Query()) != 1 {
		t.Fatal("refusing non-owned registry-environment fixture")
	}
	if _, password := u.User.Password(); password {
		t.Fatal("fixture must not read a credential")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "registry_env_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	if err = applyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var locale string
	if err = db.QueryRow(`SELECT datctype FROM pg_database WHERE datname=current_database()`).Scan(&locale); err != nil {
		t.Fatal(err)
	}
	t.Logf("owned PostgreSQL lc_ctype=%s", locale)
	return db
}

func registryEnvironmentWhitespace() []rune {
	var whitespace []rune
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.IsSpace(r) {
			whitespace = append(whitespace, r)
		}
	}
	return whitespace
}

func TestRegistryEnvironmentPostgresPublicPredicateMatchesGo(t *testing.T) {
	db := registryEnvironmentTestDB(t)
	st := NewEnvironmentStore(db)
	ctx := context.Background()
	type fixture struct {
		label string
		env   *model.Environment
	}
	var fixtures []fixture
	for _, r := range registryEnvironmentWhitespace() {
		w := string(r)
		label := fmt.Sprintf("U%04X", r)
		fixtures = append(fixtures,
			fixture{label + "-prefix-id", &model.Environment{ID: w + "registry_owned_" + label + w, Name: "ordinary"}},
			fixture{label + "-legacy-id", &model.Environment{ID: w + "_REGISTRIES" + w, Name: "ordinary"}},
			fixture{label + "-prefix-name", &model.Environment{ID: "alias-prefix-" + label, Name: w + "REGISTRY_owned" + w}},
			fixture{label + "-legacy-name", &model.Environment{ID: "alias-legacy-" + label, Name: w + "_REGISTRIES (INTERNAL)" + w}},
		)
	}
	for i, name := range []string{"ReGiStRy_owned", "REGİSTRY_owned", "_REGİSTRİES", "_REGİSTRİES (İNTERNAL)", "\tREGİSTRY_owned\u00a0", "_registries", "_registries (internal)"} {
		fixtures = append(fixtures, fixture{fmt.Sprintf("case-name-%d", i), &model.Environment{ID: fmt.Sprintf("case-alias-%d", i), Name: name}})
		if !strings.Contains(name, "(internal)") && !strings.Contains(name, "(İNTERNAL)") {
			fixtures = append(fixtures, fixture{fmt.Sprintf("case-id-%d", i), &model.Environment{ID: name, Name: "ordinary"}})
		}
	}
	for i, name := range []string{"registry", "registry-hyphen", "registries", "xregistry_owned", "_registries-more", "_registries (external)", "regıstry_owned", "\u200bregistry_owned", "\ufeffregistry_owned", "registry\u200b_owned", "\tregistry-hyphen\u00a0"} {
		fixtures = append(fixtures, fixture{fmt.Sprintf("normal-name-%d", i), &model.Environment{ID: fmt.Sprintf("normal-alias-%d", i), Name: name}}, fixture{fmt.Sprintf("normal-id-%d", i), &model.Environment{ID: name, Name: "ordinary"}})
	}
	for i, input := range fixtures {
		if input.env.Name == "ordinary" {
			input.env.Name += "-" + input.label
		}
		input.env.Order = i
		input.env.CreatedAt = time.Now().UTC()
		if err := st.Create(ctx, input.env); err != nil {
			t.Fatalf("seed %s: %v", input.label, err)
		}
	}
	all, err := st.List(ctx, 0, 0)
	if err != nil || len(all) != len(fixtures) {
		t.Fatal("internal inventory lost fixture scopes", err)
	}
	public, err := st.ListPublic(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	visible := map[string]bool{}
	for _, env := range public {
		visible[env.ID] = true
	}
	for _, input := range fixtures {
		t.Run(input.label, func(t *testing.T) {
			expected := !model.IsRegistryCredentialEnvironment(input.env)
			if visible[input.env.ID] != expected {
				t.Fatalf("SQL/Go public predicate differs: expectedPublic=%t id=%q name=%q", expected, input.env.ID, input.env.Name)
			}
		})
	}
}

func TestRegistryEnvironmentPostgresPublicPaginationExcludesAliasesFirst(t *testing.T) {
	db := registryEnvironmentTestDB(t)
	st := NewEnvironmentStore(db)
	ctx := context.Background()
	for i, r := range registryEnvironmentWhitespace() {
		if err := st.Create(ctx, &model.Environment{ID: fmt.Sprintf("alias-%03d", i), Name: string(r) + "REGISTRY_owned" + string(r), Order: 0, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Create(ctx, &model.Environment{ID: "dotted-case-alias", Name: "REGİSTRY_owned", Order: 0, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	want := []string{"public-a", "public-b", "public-c", "public-d", "public-e"}
	for _, id := range want {
		if err := st.Create(ctx, &model.Environment{ID: id, Name: "Production " + id, Order: 1, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []struct {
		limit, offset int
		expected      []string
	}{
		{1, 0, want[:1]}, {1, 1, want[1:2]}, {2, 3, want[3:]}, {100, 0, want}, {0, 0, want}, {-1, 0, want}, {2, -1, want[:2]}, {1, 100, []string{}},
	} {
		t.Run(fmt.Sprintf("limit%d-offset%d", input.limit, input.offset), func(t *testing.T) {
			rows, err := st.ListPublic(ctx, input.limit, input.offset)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, env := range rows {
				ids = append(ids, env.ID)
			}
			if !reflect.DeepEqual(ids, input.expected) {
				t.Fatalf("private alias consumed pagination: got=%v want=%v", ids, input.expected)
			}
		})
	}
}
