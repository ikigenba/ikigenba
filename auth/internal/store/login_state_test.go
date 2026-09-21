package store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

var (
	// R-4PX3-AXQC
	_ func(*Store, string, string) (LoginState, error) = (*Store).CreateLoginState
	// R-4R4Z-OPH1
	_ func(*Store, string) (LoginState, error) = (*Store).ConsumeLoginState
)

func TestCreateLoginStatePersistsInjectedIDAndExactValues(t *testing.T) {
	// R-5KEK-V79P
	random := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	st := openLoginStateTestStore(t, bytes.NewReader(random))

	tests := []struct {
		name      string
		verifier  string
		returnURL string
	}{
		{name: "values preserved", verifier: "verifier with spaces & punctuation", returnURL: "https://app.example.test/a?x=1&y=two#fragment"},
		{name: "empty values preserved", verifier: "", returnURL: ""},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if index == 1 {
				st.rand = bytes.NewReader(make([]byte, 16))
			}
			wantState := idcodec.Encode(random)
			if index == 1 {
				wantState = idcodec.Encode(make([]byte, 16))
			}

			got, err := st.CreateLoginState(tt.verifier, tt.returnURL)
			if err != nil {
				t.Fatalf("CreateLoginState() error = %v", err)
			}
			want := LoginState{State: wantState, Verifier: tt.verifier, ReturnURL: tt.returnURL}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("CreateLoginState() = %#v, want %#v", got, want)
			}

			var stored LoginState
			if err := st.db.QueryRowContext(
				context.Background(),
				`SELECT state, verifier, return_url FROM login_states WHERE state = ?`,
				got.State,
			).Scan(&stored.State, &stored.Verifier, &stored.ReturnURL); err != nil {
				t.Fatalf("read persisted login state: %v", err)
			}
			if !reflect.DeepEqual(stored, want) {
				t.Fatalf("persisted login state = %#v, want %#v", stored, want)
			}
		})
	}
}

func TestCreateLoginStateRandomFailurePersistsNothing(t *testing.T) {
	st := openLoginStateTestStore(t, bytes.NewReader(make([]byte, 15)))

	got, err := st.CreateLoginState("verifier", "https://app.example.test/")
	if err == nil {
		t.Fatalf("CreateLoginState() = %#v, nil error; want error", got)
	}
	if got != (LoginState{}) {
		t.Fatalf("CreateLoginState() on random failure = %#v, want zero value", got)
	}

	var count int
	if err := st.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM login_states`).Scan(&count); err != nil {
		t.Fatalf("count login states: %v", err)
	}
	if count != 0 {
		t.Fatalf("login state row count = %d, want 0", count)
	}
}

func TestConsumeLoginStateReturnsExactRowOnce(t *testing.T) {
	// R-5LMH-8Z0E
	st := openLoginStateTestStore(t, bytes.NewReader(make([]byte, 16)))
	want, err := st.CreateLoginState("exact verifier", "")
	if err != nil {
		t.Fatalf("CreateLoginState() error = %v", err)
	}

	got, err := st.ConsumeLoginState(want.State)
	if err != nil {
		t.Fatalf("first ConsumeLoginState() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("first ConsumeLoginState() = %#v, want %#v", got, want)
	}

	if _, err := st.ConsumeLoginState(want.State); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second ConsumeLoginState() error = %v, want ErrNotFound", err)
	}
	if _, err := st.ConsumeLoginState("unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown ConsumeLoginState() error = %v, want ErrNotFound", err)
	}
}

func TestConsumeLoginStateConcurrentCallsHaveOneWinner(t *testing.T) {
	st := openLoginStateTestStore(t, bytes.NewReader(make([]byte, 16)))
	want, err := st.CreateLoginState("concurrent verifier", "https://app.example.test/return")
	if err != nil {
		t.Fatalf("CreateLoginState() error = %v", err)
	}

	start := make(chan struct{})
	results := make(chan loginStateConsumeResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			state, err := st.ConsumeLoginState(want.State)
			results <- loginStateConsumeResult{state: state, err: err}
		}()
	}
	ready.Wait()
	close(start)

	var successes, notFound int
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			if !reflect.DeepEqual(result.state, want) {
				t.Errorf("successful ConsumeLoginState() = %#v, want %#v", result.state, want)
			}
		case errors.Is(result.err, ErrNotFound):
			notFound++
		default:
			t.Errorf("ConsumeLoginState() error = %v, want nil or ErrNotFound", result.err)
		}
	}
	if successes != 1 || notFound != 1 {
		t.Fatalf("concurrent results: %d success, %d ErrNotFound; want one each", successes, notFound)
	}
}

type loginStateConsumeResult struct {
	state LoginState
	err   error
}

func openLoginStateTestStore(t *testing.T, random io.Reader) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "auth.db"), random)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return st
}
