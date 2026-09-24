package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

var (
	_ func(string, io.Reader) (*Store, error) = Open
	_ func(*Store) error                      = (*Store).Close
)

func TestFoundationContract(t *testing.T) {
	// R-47ML-KDLX
	assertStructFields(t, User{}, []fieldSpec{
		{"ID", reflect.TypeFor[string]()},
		{"Issuer", reflect.TypeFor[string]()},
		{"Subject", reflect.TypeFor[string]()},
		{"Email", reflect.TypeFor[string]()},
		{"LastGoogleLogin", reflect.TypeFor[time.Time]()},
	})

	// R-48UH-Y5CM
	assertStructFields(t, Session{}, []fieldSpec{
		{"ID", reflect.TypeFor[string]()},
		{"UserID", reflect.TypeFor[string]()},
		{"LoginAt", reflect.TypeFor[time.Time]()},
		{"LastUsedAt", reflect.TypeFor[time.Time]()},
	})

	// R-4A2E-BX3B
	assertStructFields(t, LoginState{}, []fieldSpec{
		{"State", reflect.TypeFor[string]()},
		{"Verifier", reflect.TypeFor[string]()},
		{"ReturnURL", reflect.TypeFor[string]()},
	})

	// R-4CI7-3GKP
	assertStructFields(t, Token{}, []fieldSpec{
		{"ID", reflect.TypeFor[string]()},
		{"UserID", reflect.TypeFor[string]()},
		{"Name", reflect.TypeFor[string]()},
		{"Hash", reflect.TypeFor[string]()},
		{"Enabled", reflect.TypeFor[bool]()},
		{"CreatedAt", reflect.TypeFor[time.Time]()},
		{"ExpiresAt", reflect.TypeFor[*time.Time]()},
		{"LastUsedAt", reflect.TypeFor[*time.Time]()},
	})

	// R-4DQ3-H8BE
	assertStructFields(t, Identity{}, []fieldSpec{
		{"UserID", reflect.TypeFor[string]()},
		{"Email", reflect.TypeFor[string]()},
	})

	if ExpiryNever != "never" || Expiry30d != "30d" || Expiry90d != "90d" || Expiry365d != "365d" {
		t.Fatalf("expiry constants = %q, %q, %q, %q", ExpiryNever, Expiry30d, Expiry90d, Expiry365d)
	}

	// R-G4DV-A8BP
	if SessionIdle != 15*time.Minute {
		t.Fatalf("SessionIdle = %v", SessionIdle)
	}
	// R-G5LR-O02E
	if SessionMax != 18*time.Hour {
		t.Fatalf("SessionMax = %v", SessionMax)
	}
	// R-G81K-FJJS
	if TokenLoginWindow != 30*24*time.Hour {
		t.Fatalf("TokenLoginWindow = %v", TokenLoginWindow)
	}

	if ErrNotFound == nil || !errors.Is(ErrNotFound, ErrNotFound) || !errors.Is(errors.Join(errors.New("context"), ErrNotFound), ErrNotFound) {
		t.Fatalf("ErrNotFound is not a usable errors.Is sentinel")
	}
}

func TestOpenCreatesSchemaAndUsableStore(t *testing.T) {
	// R-4ILP-0BA6
	path := filepath.Join(t.TempDir(), "state", "auth.db")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database exists before Open: %v", err)
	}

	st, err := Open(path, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if st == nil {
		t.Fatal("Open() returned nil store")
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("created database Stat() error = %v", err)
	}
	insertFoundationRows(t, st)
	assertNoPlaintextSecretColumn(t, st)

	if err := st.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := st.db.PingContext(context.Background()); err == nil {
		t.Fatal("database remains usable after Close")
	}
}

func TestOpenExistingDatabasePreservesRows(t *testing.T) {
	// R-587L-1HUR
	path := filepath.Join(t.TempDir(), "auth.db")
	first, err := Open(path, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	insertFoundationRows(t, first)
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	second, err := Open(path, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer func() { _ = second.Close() }()

	for table, want := range map[string]int{"users": 1, "sessions": 1, "login_states": 1, "tokens": 1} {
		var got int
		if err := second.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s row count = %d, want %d", table, got, want)
		}
	}
}

func TestOpenRejectsUnwritableExistingOrdinaryDatabase(t *testing.T) {
	// R-W4JF-EHDQ
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.db")
	writable := mustOpenUsable(t, path)
	if err := writable.Close(); err != nil {
		t.Fatalf("Close(writable) error = %v", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot(database directory) error = %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	before, err := root.ReadFile("auth.db")
	if err != nil {
		t.Fatalf("ReadFile(database) error = %v", err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("Chmod(database) error = %v", err)
	}

	st, err := Open(path, bytes.NewReader(nil))
	if st != nil {
		_ = st.Close()
	}
	if st != nil || err == nil || !strings.Contains(err.Error(), "attempt to write a readonly database") {
		t.Fatalf("Open(unwritable database) = (%v, %v), want nil store and underlying readonly error", st, err)
	}
	after, err := root.ReadFile("auth.db")
	if err != nil {
		t.Fatalf("ReadFile(database after Open) error = %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("Open changed the unwritable database")
	}
}

func TestExpiryIsDefinedStringTypeWithTypedConstants(t *testing.T) {
	// R-4EXZ-V023
	const (
		_ = ExpiryNever
		_ = Expiry30d
		_ = Expiry90d
		_ = Expiry365d
	)

	expiryType := reflect.TypeFor[Expiry]()
	if expiryType.Kind() != reflect.String || expiryType.Name() != "Expiry" || expiryType.PkgPath() != "github.com/ikigenba/ikigenba/auth/internal/store" {
		t.Fatalf("Expiry type = %s kind %s pkg %q", expiryType, expiryType.Kind(), expiryType.PkgPath())
	}

	constants := []struct {
		name string
		typ  reflect.Type
		got  string
		want string
	}{
		{name: "ExpiryNever", typ: reflect.TypeOf(ExpiryNever), got: string(ExpiryNever), want: "never"},
		{name: "Expiry30d", typ: reflect.TypeOf(Expiry30d), got: string(Expiry30d), want: "30d"},
		{name: "Expiry90d", typ: reflect.TypeOf(Expiry90d), got: string(Expiry90d), want: "90d"},
		{name: "Expiry365d", typ: reflect.TypeOf(Expiry365d), got: string(Expiry365d), want: "365d"},
	}
	seen := make(map[string]string, len(constants))
	for _, constant := range constants {
		if constant.typ != expiryType || constant.got != constant.want {
			t.Errorf("%s = %q type %s, want Expiry %q", constant.name, constant.got, constant.typ, constant.want)
		}
		if previous, ok := seen[constant.got]; ok {
			t.Errorf("%s duplicates %s value %q", constant.name, previous, constant.got)
		}
		seen[constant.got] = constant.name
	}
}

func TestOpenRetainsSpecialSourceSemantics(t *testing.T) {
	// R-CEVY-E877
	work := t.TempDir()
	t.Chdir(work)
	before := dirNames(t, work)

	// modernc.org/sqlite v1.59.0: "" is journal_mode=delete; :memory: is memory.
	journalMode := func(st *Store) string {
		t.Helper()
		var mode string
		if err := st.db.QueryRowContext(context.Background(), `PRAGMA journal_mode`).Scan(&mode); err != nil {
			t.Fatalf("PRAGMA journal_mode: %v", err)
		}
		return mode
	}
	empty := mustOpenUsable(t, "")
	if got := journalMode(empty); got != "delete" {
		t.Fatalf("empty source journal_mode = %q, want delete; :memory: reports memory", got)
	}
	if err := empty.Close(); err != nil {
		t.Fatalf("Close(empty) error = %v", err)
	}
	reopened, err := Open("", bytes.NewReader(nil))
	if err != nil || reopened == nil {
		t.Fatalf("second Open(empty) = (%v, %v)", reopened, err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	var persisted int
	if err := reopened.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&persisted); err != nil {
		t.Fatalf("count users in second empty source: %v", err)
	}
	if persisted != 0 {
		t.Fatalf("empty source persisted %d users; driver temporary database was not retained", persisted)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close(second empty) error = %v", err)
	}

	memory := mustOpenUsable(t, ":memory:")
	if got := journalMode(memory); got != "memory" {
		t.Fatalf(":memory: journal_mode = %q, want memory", got)
	}
	if err := memory.Close(); err != nil {
		t.Fatalf("Close(:memory:) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, ":memory:")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(":memory: created a filesystem entry: %v", err)
	}

	shared := mustOpenUsable(t, "file::memory:?cache=shared")
	if err := shared.Close(); err != nil {
		t.Fatalf("Close(file::memory:) error = %v", err)
	}

	relativeMemory := mustOpenUsable(t, "file:mem.db?mode=memory")
	if _, err := os.Stat(filepath.Join(work, "mem.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file:mem.db?mode=memory created %v", err)
	}
	if err := relativeMemory.Close(); err != nil {
		t.Fatalf("Close(mode=memory) error = %v", err)
	}

	missingMemory := filepath.Join(t.TempDir(), "missing", "name.db")
	memoryURI := mustOpenUsable(t, "file:"+missingMemory+"?mode=memory")
	if _, err := os.Stat(filepath.Dir(missingMemory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("in-memory URI created parent %s: %v", filepath.Dir(missingMemory), err)
	}
	if err := memoryURI.Close(); err != nil {
		t.Fatalf("Close(memory URI) error = %v", err)
	}

	if got := dirNames(t, work); !reflect.DeepEqual(got, before) {
		t.Fatalf("special sources changed %s entries from %v to %v", work, before, got)
	}

	path := filepath.Join(t.TempDir(), "ok.db")
	created := mustOpenUsable(t, path)
	if err := created.Close(); err != nil {
		t.Fatalf("Close(ok.db) error = %v", err)
	}
	readonly, err := Open("file:"+path+"?mode=ro", bytes.NewReader(nil))
	if err != nil || readonly == nil {
		t.Fatalf("Open(mode=ro) = (%v, %v)", readonly, err)
	}
	t.Cleanup(func() { _ = readonly.Close() })
	var users int
	if err := readonly.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("mode=ro count: %v", err)
	}
	if users != 1 {
		t.Fatalf("mode=ro users = %d, want the database written without the query", users)
	}
	if _, err := readonly.db.ExecContext(context.Background(), `INSERT INTO users (id, issuer, subject, email, last_google_login) VALUES ('u2', 'i', 's', 'e', 1)`); err == nil || !strings.Contains(err.Error(), "readonly") {
		t.Fatalf("mode=ro insert error = %v, want the URI query to keep the database readonly", err)
	}

	blocked := filepath.Join(t.TempDir(), "nope", "a.db")
	st, err := Open("file:"+blocked+"?mode=rwc", bytes.NewReader(nil))
	if err == nil || st != nil {
		if st != nil {
			_ = st.Close()
		}
		t.Fatalf("Open(file URI with missing parent) = (%v, %v), want the driver error and no created parent", st, err)
	}
	if _, err := os.Stat(filepath.Dir(blocked)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file: URI created parent directory %s", filepath.Dir(blocked))
	}

	querylessParent := filepath.Join(t.TempDir(), "noquery")
	querylessPath := filepath.Join(querylessParent, "a.db")
	queryless := "file:" + querylessPath
	if strings.Contains(queryless, "?") {
		t.Fatalf("query-less file URI fixture contains a query: %s", queryless)
	}
	if _, err := os.Stat(querylessParent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("query-less file URI parent exists before Open: %v", err)
	}
	st, err = Open(queryless, bytes.NewReader(nil))
	if st != nil {
		_ = st.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "unable to open database file") {
		t.Fatalf("Open(%s) = (%v, %v), want the driver rejection of a missing file and no created parent", queryless, st, err)
	}
	if _, err := os.Stat(querylessParent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("query-less file: URI created parent directory %s", querylessParent)
	}
	if _, err := os.Stat(querylessPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("query-less file: URI created database file %s", querylessPath)
	}
	if got := dirNames(t, work); !reflect.DeepEqual(got, before) {
		t.Fatalf("query-less file: URI changed %s entries from %v to %v", work, before, got)
	}

	relative := mustOpenUsable(t, filepath.Join("state", "auth.db"))
	if _, err := os.Stat(filepath.Join(work, "state", "auth.db")); err != nil {
		t.Fatalf("relative ordinary path was not created in the working directory: %v", err)
	}
	if err := relative.Close(); err != nil {
		t.Fatalf("Close(relative) error = %v", err)
	}
}

func TestOpenCreatesDatabaseForOrdinaryPaths(t *testing.T) {
	// R-CG3U-RZXW
	base := t.TempDir()
	absentParent := filepath.Join(base, "missing", "nested", "auth.db")
	if _, err := os.Stat(filepath.Join(base, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent exists before Open: %v", err)
	}
	absent := mustOpenUsable(t, absentParent)
	if _, err := os.Stat(absentParent); err != nil {
		t.Fatalf("database created with a missing parent Stat() = %v", err)
	}
	if err := absent.Close(); err != nil {
		t.Fatalf("Close(absent parent) error = %v", err)
	}

	parent := filepath.Join(base, "present")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("Mkdir(parent) error = %v", err)
	}
	presentPath := filepath.Join(parent, "auth.db")
	if _, err := os.Stat(presentPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database exists before Open: %v", err)
	}
	present := mustOpenUsable(t, presentPath)
	if _, err := os.Stat(presentPath); err != nil {
		t.Fatalf("database created with an existing parent Stat() = %v", err)
	}
	if err := present.Close(); err != nil {
		t.Fatalf("Close(existing parent) error = %v", err)
	}

	work := t.TempDir()
	t.Chdir(work)
	relativeAbsent := filepath.Join("rel", "absent", "auth.db")
	opened := mustOpenUsable(t, relativeAbsent)
	if _, err := os.Stat(filepath.Join(work, relativeAbsent)); err != nil {
		t.Fatalf("relative path with a missing parent was not created in the working directory: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("Close(relative absent) error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(work, "have"), 0o700); err != nil {
		t.Fatalf("Mkdir(have) error = %v", err)
	}
	relativePresent := filepath.Join("have", "auth.db")
	opened = mustOpenUsable(t, relativePresent)
	if _, err := os.Stat(filepath.Join(work, relativePresent)); err != nil {
		t.Fatalf("relative path with an existing parent was not created in the working directory: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("Close(relative present) error = %v", err)
	}
}

func TestOpenDirectoryModes(t *testing.T) {
	// R-CIJN-JJFA
	// Umask 0 makes the stored mode equal the mode passed to mkdir, so 0700
	// is distinguishable from a looser creation mode such as 0777.
	base := t.TempDir()
	old := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(old) })

	existing := filepath.Join(base, "existing")
	if err := os.Mkdir(existing, 0o750); err != nil {
		t.Fatalf("Mkdir(existing) error = %v", err)
	}
	created := filepath.Join(existing, "new1", "new2", "auth.db")
	st := mustOpenUsable(t, created)
	if got := dirPerm(t, existing); got != 0o750 {
		t.Fatalf("existing directory mode = %#o, want unchanged 0750", got)
	}
	if got := dirPerm(t, filepath.Join(existing, "new1")); got != 0o700 {
		t.Fatalf("created directory new1 mode = %#o, want 0700", got)
	}
	if got := dirPerm(t, filepath.Join(existing, "new1", "new2")); got != 0o700 {
		t.Fatalf("created directory new2 mode = %#o, want 0700", got)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	direct := filepath.Join(base, "direct")
	if err := os.Mkdir(direct, 0o740); err != nil {
		t.Fatalf("Mkdir(direct) error = %v", err)
	}
	directDB := mustOpenUsable(t, filepath.Join(direct, "auth.db"))
	if got := dirPerm(t, direct); got != 0o740 {
		t.Fatalf("existing parent mode = %#o, want unchanged 0740", got)
	}
	if err := directDB.Close(); err != nil {
		t.Fatalf("Close(direct) error = %v", err)
	}
}

func TestOpenReportsCreationAndDatabaseFailures(t *testing.T) {
	// R-CHBR-5ROL
	base := t.TempDir()
	blocker := filepath.Join(base, "not-a-dir")
	payload := []byte("leave me")
	if err := os.WriteFile(blocker, payload, 0o600); err != nil {
		t.Fatalf("WriteFile(blocker) error = %v", err)
	}
	beforeInfo, err := os.Stat(blocker)
	if err != nil {
		t.Fatalf("Stat(blocker) error = %v", err)
	}
	beforeFiles := filesUnder(t, base)
	st, err := Open(filepath.Join(blocker, "nested", "auth.db"), bytes.NewReader(nil))
	if err == nil || st != nil || !strings.Contains(err.Error(), "not a directory") {
		if st != nil {
			_ = st.Close()
		}
		t.Fatalf("Open(file occupying directory) = (%v, %v), want nil store and an error containing the directory failure", st, err)
	}
	got, err := os.ReadFile(filepath.Clean(blocker))
	if err != nil {
		t.Fatalf("ReadFile(blocker) error = %v", err)
	}
	afterInfo, err := os.Stat(blocker)
	if err != nil {
		t.Fatalf("Stat(blocker) after Open error = %v", err)
	}
	if !bytes.Equal(got, payload) || !afterInfo.Mode().IsRegular() || afterInfo.Mode() != beforeInfo.Mode() || afterInfo.Size() != beforeInfo.Size() {
		t.Fatalf("blocker changed: bytes %q mode %v size %d", got, afterInfo.Mode(), afterInfo.Size())
	}
	if after := filesUnder(t, base); !reflect.DeepEqual(after, beforeFiles) {
		t.Fatalf("Open created a database despite the blocking file: before %v after %v", beforeFiles, after)
	}

	asDir := filepath.Join(base, "directory.db")
	if err := os.Mkdir(asDir, 0o700); err != nil {
		t.Fatalf("Mkdir(directory) error = %v", err)
	}
	st, err = Open(asDir, bytes.NewReader(nil))
	if err == nil || st != nil || !strings.Contains(err.Error(), "unable to open database file") {
		if st != nil {
			_ = st.Close()
		}
		t.Fatalf("Open(directory) = (%v, %v), want nil store and the driver open failure", st, err)
	}
	if names := dirNames(t, asDir); len(names) != 0 {
		t.Fatalf("Open(directory) created %v", names)
	}

	readonlyPath := filepath.Join(base, "readonly.db")
	createBareSQLite(t, readonlyPath)
	readonlyBefore, err := os.Stat(readonlyPath)
	if err != nil {
		t.Fatalf("Stat(readonly) error = %v", err)
	}
	st, err = Open("file:"+readonlyPath+"?mode=ro", bytes.NewReader(nil))
	if err == nil || st != nil || !strings.Contains(err.Error(), "attempt to write a readonly database") {
		if st != nil {
			_ = st.Close()
		}
		t.Fatalf("Open(readonly database) = (%v, %v), want nil store and the schema failure", st, err)
	}
	readonlyAfter, err := os.Stat(readonlyPath)
	if err != nil {
		t.Fatalf("Stat(readonly) after Open error = %v", err)
	}
	if readonlyAfter.Size() != readonlyBefore.Size() || readonlyAfter.Mode() != readonlyBefore.Mode() {
		t.Fatalf("readonly database changed size %d->%d mode %v->%v", readonlyBefore.Size(), readonlyAfter.Size(), readonlyBefore.Mode(), readonlyAfter.Mode())
	}

	bad := filepath.Join(base, "bad.db")
	badPayload := []byte("this is not sqlite")
	if err := os.WriteFile(bad, badPayload, 0o600); err != nil {
		t.Fatalf("WriteFile(bad) error = %v", err)
	}
	st, err = Open(bad, bytes.NewReader(nil))
	if err == nil || st != nil || !strings.Contains(err.Error(), "file is not a database") {
		if st != nil {
			_ = st.Close()
		}
		t.Fatalf("Open(non-database) = (%v, %v), want nil store and the driver failure", st, err)
	}
	got, err = os.ReadFile(filepath.Clean(bad))
	if err != nil {
		t.Fatalf("ReadFile(bad) error = %v", err)
	}
	if !bytes.Equal(got, badPayload) {
		t.Fatalf("non-database bytes = %q, want unchanged %q", got, badPayload)
	}
}

func TestErrNotFoundForMissingAndOtherOwnerRows(t *testing.T) {
	// R-4HDS-MJJH
	st := openTokenTestStore(t, bytes.NewReader(nil))
	now := tokenTestNow()
	insertTokenUser(t, st, "owner", "owner@example.com", now)
	insertTokenUser(t, st, "other", "other@example.com", now)
	insertToken(t, st, Token{
		ID:        "owned",
		UserID:    "owner",
		Name:      "owned",
		Hash:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Enabled:   true,
		CreatedAt: now,
	})
	insertSessionFixture(t, st, "kept-session", "owner", now, now)
	if _, err := st.db.ExecContext(
		context.Background(),
		`INSERT INTO login_states (state, verifier, return_url) VALUES (?, ?, ?)`,
		"kept-state",
		"verifier",
		"/return",
	); err != nil {
		t.Fatalf("insert login state: %v", err)
	}
	beforeTokens := allTokenStates(t, st)

	if got, err := st.LookupSessionIdentity("missing-session", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("LookupSessionIdentity(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if got, err := st.TouchSession("missing-session", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("TouchSession(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if got, err := st.ConsumeLoginState("missing-state"); err == nil || !errors.Is(err, ErrNotFound) || got != (LoginState{}) {
		t.Errorf("ConsumeLoginState(missing) = %#v, %v; want zero login state and ErrNotFound", got, err)
	}
	if got, err := st.LookupTokenIdentity("missing-secret", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("LookupTokenIdentity(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if got, err := st.TouchTokenIdentity("missing-secret", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("TouchTokenIdentity(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if err := st.SetTokenEnabled("owner", "missing-token", false); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("SetTokenEnabled(missing) error = %v, want ErrNotFound", err)
	}
	if err := st.SetTokenEnabled("other", "owned", false); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("SetTokenEnabled(other owner) error = %v, want ErrNotFound", err)
	}
	if err := st.DeleteToken("owner", "missing-token"); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteToken(missing) error = %v, want ErrNotFound", err)
	}
	if err := st.DeleteToken("other", "owned"); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteToken(other owner) error = %v, want ErrNotFound", err)
	}

	if after := allTokenStates(t, st); !reflect.DeepEqual(after, beforeTokens) {
		t.Errorf("missing and other-owner calls changed tokens: before %#v, after %#v", beforeTokens, after)
	}
	assertStoredSession(t, st, "kept-session", "owner", now.UnixNano(), now.UnixNano())
	var verifier, returnURL string
	if err := st.db.QueryRowContext(
		context.Background(),
		`SELECT verifier, return_url FROM login_states WHERE state = ?`,
		"kept-state",
	).Scan(&verifier, &returnURL); err != nil || verifier != "verifier" || returnURL != "/return" {
		t.Errorf("kept login state = (%q, %q, %v), want verifier and /return", verifier, returnURL, err)
	}
}

type fieldSpec struct {
	name string
	typ  reflect.Type
}

func assertStructFields(t *testing.T, value any, want []fieldSpec) {
	t.Helper()
	typ := reflect.TypeOf(value)
	if typ.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typ, typ.NumField(), len(want))
	}
	for i, field := range want {
		got := typ.Field(i)
		if got.Name != field.name || got.Type != field.typ || !got.IsExported() {
			t.Fatalf("%s field %d = %s %v (exported %v), want %s %v", typ, i, got.Name, got.Type, got.IsExported(), field.name, field.typ)
		}
	}
}

func mustOpenUsable(t *testing.T, source string) *Store {
	t.Helper()
	st, err := Open(source, bytes.NewReader(nil))
	if err != nil || st == nil {
		t.Fatalf("Open(%q) = (%v, %v)", source, st, err)
	}
	t.Cleanup(func() { _ = st.Close() })
	insertFoundationRows(t, st)
	var users int
	if err := st.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("Open(%q) count users: %v", source, err)
	}
	if users != 1 {
		t.Fatalf("Open(%q) users = %d, want 1", source, users)
	}
	return st
}

func createBareSQLite(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(%s) error = %v", path, err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(context.Background(), `PRAGMA user_version = 1`); err != nil {
		t.Fatalf("create bare sqlite database: %v", err)
	}
}

func dirNames(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("ReadDir(%s) error = %v", path, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func dirPerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
	return info.Mode().Perm()
}

func filesUnder(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(found)
	return found
}

func insertFoundationRows(t *testing.T, st *Store) {
	t.Helper()
	statements := []string{
		`INSERT INTO users (id, issuer, subject, email, last_google_login) VALUES ('user', 'issuer', 'subject', 'member@example.com', 100)`,
		`INSERT INTO sessions (id, user_id, login_at, last_used_at) VALUES ('session', 'user', 100, 200)`,
		`INSERT INTO login_states (state, verifier, return_url) VALUES ('state', 'verifier', '')`,
		`INSERT INTO tokens (id, user_id, name, hash, enabled, created_at, expires_at, last_used_at) VALUES ('token', 'user', 'deploy', '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef', 1, 100, NULL, NULL)`,
	}
	for _, statement := range statements {
		if _, err := st.db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("schema insert error = %v", err)
		}
	}
}

func assertNoPlaintextSecretColumn(t *testing.T, st *Store) {
	t.Helper()
	rows, err := st.db.QueryContext(context.Background(), `PRAGMA table_info(tokens)`)
	if err != nil {
		t.Fatalf("token schema query error = %v", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("token schema scan error = %v", err)
		}
		if name == "secret" || name == "plaintext_secret" {
			t.Fatalf("tokens schema persists plaintext in column %q", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("token schema rows error = %v", err)
	}
}
