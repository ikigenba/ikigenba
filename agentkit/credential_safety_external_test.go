package agentkit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	ak "github.com/ikigenba/ikigenba/agentkit"
)

const safetyValue = "loopback-example-0123456789"

type safetyTransport func(*http.Request) (*http.Response, error)

func (f safetyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func safetyClient(t *testing.T, f safetyTransport) {
	t.Helper()
	previous := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: f}
	t.Cleanup(func() { http.DefaultClient = previous })
}

func safetyResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func safetyConversation(t *testing.T, wire ak.WireFormat, rotator ak.Rotator, log *bytes.Buffer, rotation ak.Rotation) *ak.Conversation {
	t.Helper()
	auth, err := (ak.Offering{ID: ak.OfferingAnthropicMessages, WireFormat: wire, Endpoints: []ak.EndpointSpec{{AuthMode: rotator.AuthMode(), Rotation: rotation}}}).Authenticator(rotator)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := ak.NewEndpoint(auth, ak.WithBaseURL("http://127.0.0.1/provider"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := ak.Config{Settings: ak.Settings{Options: ak.Options{"max_output_tokens": "32"}}}
	if log != nil {
		cfg.Log = ak.NewLog(log, func() time.Time { return time.Time{} }, "test")
	}
	c, err := ak.New(wire, endpoint, "model", cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func safetySend(ctx context.Context, c *ak.Conversation) error {
	stream := c.Send(ctx, ak.Text{Text: "hello"})
	for event := range stream.Events() {
		_ = event
	}
	return stream.Err()
}

func safetyCheckChain(t *testing.T, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected failure")
	}
	var visit func(error)
	visit = func(e error) {
		if e == nil {
			return
		}
		for _, secret := range secrets {
			if strings.Contains(e.Error(), secret) {
				t.Fatal("error chain disclosed a secret")
			}
		}
		switch e := any(e).(type) {
		case interface{ Unwrap() []error }:
			for _, cause := range e.Unwrap() {
				visit(cause)
			}
		case interface{ Unwrap() error }:
			visit(e.Unwrap())
		}
	}
	visit(err)
}

func safetyCheckLog(t *testing.T, log *bytes.Buffer, secrets ...string) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(log.Bytes()))
	found := false
	var check func(any)
	check = func(v any) {
		switch v := v.(type) {
		case string:
			for _, secret := range secrets {
				if strings.Contains(v, secret) {
					t.Fatal("decoded error log disclosed a secret")
				}
			}
		case []any:
			for _, value := range v {
				check(value)
			}
		case map[string]any:
			for key, value := range v {
				check(key)
				check(value)
			}
		}
	}
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["type"] == string(ak.RecordError) {
			found = true
			payload, ok := record["error"].(map[string]any)
			if !ok {
				t.Fatal("error log record has no error object")
			}
			check(payload)
		}
	}
	if !found {
		t.Fatal("missing error log record")
	}
}

type safetyReader struct{ err error }

func (r safetyReader) Read([]byte) (int, error) { return 0, r.err }
func (safetyReader) Close() error               { return nil }

// R-T6FE-G1JX R-VZ8C-D8NY R-F0WI-DARB R-SZ40-5F3R R-URHJ-5JWN
func TestCredentialSafetyProviderFailures(t *testing.T) {
	wires := []struct {
		name    string
		newWire func() ak.WireFormat
		frame   string
	}{
		{"anthropic", ak.AnthropicMessagesWire, `{"type":"error","error":{"type":"SECRET","message":"before SECRET after SECRET"}}`},
		{"chat", ak.ChatWire, `{"error":{"code":"SECRET","message":"before SECRET after SECRET"}}`},
		{"responses", ak.ResponsesWire, `{"type":"error","code":"SECRET","message":"before SECRET after SECRET"}`},
		{"openai-chat", ak.OpenAIChatWire, `{"error":{"code":"SECRET","message":"before SECRET after SECRET"}}`},
		{"openai-responses", ak.OpenAIResponsesWire, `{"type":"response.failed","response":{"error":{"code":"SECRET","message":"before SECRET after SECRET"}}}`},
		{"xai-chat", ak.XAIChatWire, `{"error":{"code":"SECRET","message":"before SECRET after SECRET"}}`},
		{"xai-responses", ak.XAIResponsesWire, `{"type":"error","code":"SECRET","message":"before SECRET after SECRET"}`},
		{"gemini", ak.GeminiGenerateContentWire, ""},
	}
	const mark string = ak.RedactionMark
	for _, wire := range wires {
		for _, mode := range []string{"transport", "non2xx", "body-non2xx", "body-stream", "inband"} {
			if mode == "inband" && wire.frame == "" {
				continue
			}
			t.Run(wire.name+"/"+mode, func(t *testing.T) {
				safetyClient(t, func(r *http.Request) (*http.Response, error) {
					if strings.Contains(r.URL.String(), safetyValue) {
						t.Fatal("request URL disclosed secret")
					}
					cause := errors.Join(fmt.Errorf("nested %s", safetyValue), fmt.Errorf("again %s", safetyValue))
					switch mode {
					case "transport":
						return nil, cause
					case "non2xx":
						return safetyResponse(422, "before "+safetyValue+" after "+safetyValue), nil
					case "inband":
						return safetyResponse(200, "data: "+strings.ReplaceAll(wire.frame, "SECRET", safetyValue)+"\n\n"), nil
					default:
						response := safetyResponse(200, "")
						if mode == "body-non2xx" {
							response.StatusCode = 422
						}
						response.Body = safetyReader{err: cause}
						return response, nil
					}
				})
				var log bytes.Buffer
				c := safetyConversation(t, wire.newWire(), ak.APIKeyRotator(safetyValue), &log, ak.Rotation{})
				err := safetySend(context.Background(), c)
				safetyCheckChain(t, err, safetyValue)
				safetyCheckLog(t, &log, safetyValue)
				if mode == "non2xx" || mode == "inband" {
					var providerErr *ak.Error
					if !errors.As(err, &providerErr) {
						t.Fatal("missing provider Error")
					}
					if providerErr.Message != "before "+mark+" after "+mark {
						t.Fatal("vendor message bytes not preserved")
					}
					if mode == "inband" && providerErr.Code != mark {
						t.Fatal("vendor code not redacted")
					}
				}
			})
		}
	}
}

// R-T3ZL-OI2J
func TestCredentialSafetyPreservesContextCauses(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			safetyClient(t, func(r *http.Request) (*http.Response, error) {
				return nil, errors.Join(errors.New(safetyValue), r.Context().Err())
			})
			ctx, cancel := context.WithCancel(context.Background())
			expected := context.Canceled
			if deadline {
				ctx, cancel = context.WithDeadline(context.Background(), time.Unix(1, 0))
				expected = context.DeadlineExceeded
			} else {
				cancel()
			}
			defer cancel()
			c := safetyConversation(t, ak.ChatWire(), ak.APIKeyRotator(safetyValue), nil, ak.Rotation{})
			err := safetySend(ctx, c)
			if !errors.Is(err, expected) {
				t.Fatal("context cause lost")
			}
			safetyCheckChain(t, err, safetyValue)
		})
	}
}

type safetyStore struct {
	data              []byte
	readErr, writeErr error
}

func (s *safetyStore) Read(context.Context) ([]byte, error) { return s.data, s.readErr }
func (s *safetyStore) Write(_ context.Context, data []byte) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.data = data
	return nil
}

// R-EYGP-LR9X R-F0WI-DARB R-SZ40-5F3R
func TestCredentialSafetyOAuthRefresh(t *testing.T) {
	const access = "access-token-0123456789"
	const refresh = access + "-long-refresh"
	const responseSecret = "response-token-0123456789"
	for _, mode := range []string{"transport", "non2xx", "unusable", "body", "build"} {
		t.Run(mode, func(t *testing.T) {
			safetyClient(t, func(*http.Request) (*http.Response, error) {
				if mode == "transport" {
					return nil, errors.Join(errors.New(access), errors.New(refresh))
				}
				if mode == "body" {
					response := safetyResponse(200, "")
					response.Body = safetyReader{err: errors.New(refresh)}
					return response, nil
				}
				fields := map[string]any{"error": refresh + " / " + access + " / " + responseSecret, "error_description": "prefix " + refresh + " / " + access + " / " + responseSecret + " suffix", "refresh_token": responseSecret}
				body, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				status := 401
				if mode == "unusable" {
					status = 200
				}
				return safetyResponse(status, string(body)), nil
			})
			data, err := json.Marshal(map[string]string{"access_token": access, "refresh_token": refresh})
			if err != nil {
				t.Fatal(err)
			}
			store := &safetyStore{data: data}
			rotation := ak.Rotation{RefreshURL: "http://127.0.0.1/refresh"}
			if mode == "build" {
				rotation.RefreshURL = "http://127.0.0.1/%" + refresh
			}
			_, err = ak.OAuthRotator(store).Rotate(context.Background(), rotation)
			safetyCheckChain(t, err, access, refresh, responseSecret)
			if mode == "non2xx" {
				var e *ak.Error
				if !errors.As(err, &e) {
					t.Fatal("missing OAuth Error")
				}
				const mark string = ak.RedactionMark
				if e.Code != mark+" / "+mark+" / "+mark || e.Message != "prefix "+mark+" / "+mark+" / "+mark+" suffix" {
					t.Fatal("OAuth vendor text not preserved with longest secrets first")
				}
			}
		})
	}
	for _, write := range []bool{false, true} {
		t.Run("store-"+fmt.Sprint(write), func(t *testing.T) {
			sentinel := errors.New(refresh)
			store := &safetyStore{data: []byte(`{"access_token":"` + access + `","refresh_token":"` + refresh + `"}`), readErr: sentinel}
			if write {
				store.readErr = nil
				store.writeErr = sentinel
				safetyClient(t, func(*http.Request) (*http.Response, error) {
					return safetyResponse(200, `{"access_token":"new-token-0123456789"}`), nil
				})
			}
			_, err := ak.OAuthRotator(store).Rotate(context.Background(), ak.Rotation{RefreshURL: "http://127.0.0.1/refresh"})
			if any(err) != any(sentinel) {
				t.Fatal("store error not passed through unchanged")
			}
		})
	}
}

// R-W0G8-R0EN R-VZ8C-D8NY R-URHJ-5JWN
func TestCredentialSafetyGeminiCacheLifecycle(t *testing.T) {
	for _, operation := range []string{"create", "release", "close"} {
		for _, mode := range []string{"transport", "non2xx"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				safetyClient(t, func(r *http.Request) (*http.Response, error) {
					if strings.Contains(r.URL.String(), safetyValue) {
						t.Fatal("cache request URL disclosed secret")
					}
					if r.Header.Get("x-goog-api-key") != safetyValue {
						t.Fatal("cache request missing key header")
					}
					failing := (operation == "create" && r.Method == http.MethodPost && strings.Contains(r.URL.Path, "cachedContents")) || (operation != "create" && r.Method == http.MethodDelete)
					if failing {
						if mode == "transport" {
							return nil, errors.Join(errors.New(safetyValue), fmt.Errorf("wrapped: %w", errors.New(safetyValue)))
						}
						return safetyResponse(422, "before "+safetyValue+" after"), nil
					}
					if strings.Contains(r.URL.Path, "cachedContents") {
						return safetyResponse(200, `{"name":"cachedContents/cache"}`), nil
					}
					return safetyResponse(200, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n"), nil
				})
				var log bytes.Buffer
				c := safetyConversation(t, ak.GeminiGenerateContentWire(), ak.APIKeyRotator(safetyValue), &log, ak.Rotation{})
				if err := c.AddSystem("prefix"); err != nil {
					t.Fatal(err)
				}
				sp, err := c.Savepoint()
				if err != nil {
					t.Fatal(err)
				}
				err = safetySend(context.Background(), c)
				if operation != "create" {
					if err != nil {
						t.Fatal(err)
					}
					if operation == "release" {
						err = c.Release(sp)
					} else {
						err = c.Close()
					}
				}
				safetyCheckChain(t, err, safetyValue)
				if operation == "create" {
					safetyCheckLog(t, &log, safetyValue)
				}
			})
		}
	}
}

// R-URHJ-5JWN
func TestCredentialSafetyOAuthRefreshEventLog(t *testing.T) {
	const access = "oauth-access-0123456789"
	const refresh = "oauth-refresh-0123456789"
	const minted = "oauth-response-0123456789"
	for _, mode := range []string{"transport", "non2xx", "unusable", "body", "build"} {
		t.Run(mode, func(t *testing.T) {
			safetyClient(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/refresh" {
					return safetyResponse(401, "rejected"), nil
				}
				if mode == "transport" {
					return nil, errors.Join(errors.New(access), errors.New(refresh))
				}
				if mode == "body" {
					response := safetyResponse(200, "")
					response.Body = safetyReader{err: errors.Join(errors.New(access), errors.New(refresh))}
					return response, nil
				}
				body := `{"error":"` + refresh + `","error_description":"` + access + ` / ` + minted + `","refresh_token":"` + minted + `"}`
				status := 401
				if mode == "unusable" {
					status = 200
				}
				return safetyResponse(status, body), nil
			})
			store := &safetyStore{data: []byte(`{"access_token":"` + access + `","refresh_token":"` + refresh + `"}`)}
			var log bytes.Buffer
			rotation := ak.Rotation{RefreshURL: "http://127.0.0.1/refresh"}
			if mode == "build" {
				rotation.RefreshURL = "http://127.0.0.1/%" + refresh
			}
			c := safetyConversation(t, ak.ChatWire(), ak.OAuthRotator(store), &log, rotation)
			err := safetySend(context.Background(), c)
			safetyCheckChain(t, err, access, refresh, minted)
			safetyCheckLog(t, &log, access, refresh, minted)
		})
	}
}

type safetySequenceRotator struct {
	values []string
	calls  int
	mode   ak.AuthMode
}

func (r *safetySequenceRotator) AuthMode() ak.AuthMode { return r.mode }
func (r *safetySequenceRotator) Token(context.Context) (ak.Token, error) {
	value := r.values[r.calls]
	r.calls++
	return ak.Token{Bearer: value}, nil
}
func (*safetySequenceRotator) Rotate(context.Context, ak.Rotation) (ak.Token, error) {
	return ak.Token{}, errors.New("unexpected rotation")
}

// R-F0WI-DARB R-T6FE-G1JX R-URHJ-5JWN
func TestCredentialSafetyRotatedBearerOverlap(t *testing.T) {
	const short = "loopback-example-0123456789"
	const long = short + "-extended"
	for _, mode := range []ak.AuthMode{ak.AuthModeAPIKey, ak.AuthModeOAuth} {
		t.Run(string(mode), func(t *testing.T) {
			calls := 0
			safetyClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return safetyResponse(200, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"), nil
				}
				return safetyResponse(422, "prefix "+long+" / "+short+" / "+long+" suffix"), nil
			})
			var log bytes.Buffer
			c := safetyConversation(t, ak.ChatWire(), &safetySequenceRotator{values: []string{long, short}, mode: mode}, &log, ak.Rotation{})
			if err := safetySend(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			err := safetySend(context.Background(), c)
			safetyCheckChain(t, err, long, short)
			safetyCheckLog(t, &log, long, short)
			var e *ak.Error
			if !errors.As(err, &e) {
				t.Fatal("missing provider Error")
			}
			want := "prefix " + ak.RedactionMark + " / " + ak.RedactionMark + " / " + ak.RedactionMark + " suffix"
			if e.Message != want {
				t.Fatal("overlapping bearer redaction changed surrounding vendor bytes")
			}
		})
	}
}

// R-T3ZL-OI2J R-T6FE-G1JX
func TestCredentialSafetyContextSentinelText(t *testing.T) {
	value := context.Canceled.Error()
	safetyClient(t, func(*http.Request) (*http.Response, error) { return nil, context.Canceled })
	c := safetyConversation(t, ak.ChatWire(), ak.APIKeyRotator(value), nil, ak.Rotation{})
	err := safetySend(context.Background(), c)
	safetyCheckChain(t, err, value)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("redacted context cause identity lost")
	}
}

// R-URHJ-5JWN
func TestCredentialSafetyOrdinaryOAuthProviderLog(t *testing.T) {
	const access = "ordinary-access-0123456789"
	const refresh = "ordinary-refresh-0123456789"
	for _, frame := range []bool{false, true} {
		t.Run(fmt.Sprint(frame), func(t *testing.T) {
			safetyClient(t, func(*http.Request) (*http.Response, error) {
				if frame {
					return safetyResponse(200, "data: "+`{"error":{"code":"vendor-code","message":"`+refresh+`"}}`+"\n\n"), nil
				}
				return safetyResponse(422, refresh), nil
			})
			store := &safetyStore{data: []byte(`{"access_token":"` + access + `","refresh_token":"` + refresh + `"}`)}
			var log bytes.Buffer
			c := safetyConversation(t, ak.ChatWire(), ak.OAuthRotator(store), &log, ak.Rotation{})
			err := safetySend(context.Background(), c)
			if err == nil {
				t.Fatal("missing provider error")
			}
			safetyCheckLog(t, &log, access, refresh)
		})
	}
}

// R-URHJ-5JWN R-T8UR-8BFL
func TestCredentialSafetyErrorMemberPreservesLogID(t *testing.T) {
	const access = `log-"access\0123456789`
	const refresh = `log-"refresh\0123456789`
	for _, oauth := range []bool{false, true} {
		t.Run(fmt.Sprint(oauth), func(t *testing.T) {
			safetyClient(t, func(*http.Request) (*http.Response, error) {
				return safetyResponse(422, access+" / "+refresh), nil
			})
			wire := ak.ChatWire()
			rotator := ak.APIKeyRotator(access)
			model := "model"
			secrets := []string{access}
			if oauth {
				data, err := json.Marshal(map[string]string{"access_token": access, "refresh_token": refresh})
				if err != nil {
					t.Fatal(err)
				}
				rotator = ak.OAuthRotator(&safetyStore{data: data})
				model = refresh
				secrets = append(secrets, refresh)
			}
			auth, err := (ak.Offering{ID: ak.OfferingAnthropicMessages, WireFormat: wire, Endpoints: []ak.EndpointSpec{{AuthMode: rotator.AuthMode()}}}).Authenticator(rotator)
			if err != nil {
				t.Fatal(err)
			}
			endpoint, err := ak.NewEndpoint(auth, ak.WithBaseURL("http://127.0.0.1/provider"))
			if err != nil {
				t.Fatal(err)
			}
			var log bytes.Buffer
			id := "consumer " + access + " / " + refresh
			c, err := ak.New(wire, endpoint, model, ak.Config{Log: ak.NewLog(&log, func() time.Time { return time.Time{} }, id)})
			if err != nil {
				t.Fatal(err)
			}
			if err := safetySend(context.Background(), c); err == nil {
				t.Fatal("missing provider error")
			}
			safetyCheckLog(t, &log, secrets...)
			decoder := json.NewDecoder(bytes.NewReader(log.Bytes()))
			for decoder.More() {
				var record ak.LogRecord
				if err := decoder.Decode(&record); err != nil {
					t.Fatal(err)
				}
				if record.ID != id {
					t.Fatal("log ID was not preserved verbatim")
				}
			}
		})
	}
}

type safetyProactiveRotator struct{ initial, updated string }

func (*safetyProactiveRotator) AuthMode() ak.AuthMode { return ak.AuthModeOAuth }
func (r *safetyProactiveRotator) Token(context.Context) (ak.Token, error) {
	return ak.Token{Bearer: r.initial, ExpiresAt: time.Unix(1, 0)}, nil
}
func (r *safetyProactiveRotator) Rotate(context.Context, ak.Rotation) (ak.Token, error) {
	return ak.Token{Bearer: r.updated}, nil
}

// R-T6FE-G1JX R-F0WI-DARB R-URHJ-5JWN
func TestCredentialSafetyProactiveRotationReturnedBearer(t *testing.T) {
	const previous = "old-returned-0123456789"
	const current = "new-returned-0123456789"
	safetyClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+current {
			t.Fatal("updated token not presented")
		}
		return safetyResponse(422, "before "+previous+" / "+current+" after"), nil
	})
	var log bytes.Buffer
	c := safetyConversation(t, ak.ChatWire(), &safetyProactiveRotator{initial: previous, updated: current}, &log, ak.Rotation{})
	err := safetySend(context.Background(), c)
	safetyCheckChain(t, err, previous, current)
	safetyCheckLog(t, &log, previous, current)
	var e *ak.Error
	if !errors.As(err, &e) {
		t.Fatal("missing provider Error")
	}
	if e.Message != "before "+ak.RedactionMark+" / "+ak.RedactionMark+" after" {
		t.Fatal("old returned bearer not replaced verbatim")
	}
}

// R-VZ8C-D8NY R-W0G8-R0EN
func TestCredentialSafetyGeminiResourceName(t *testing.T) {
	for _, operation := range []string{"release", "close"} {
		t.Run(operation, func(t *testing.T) {
			deletes := 0
			safetyClient(t, func(r *http.Request) (*http.Response, error) {
				if strings.Contains(r.URL.String(), safetyValue) || strings.Contains(r.URL.Path, safetyValue) {
					t.Fatal("resource name disclosed credential in URL")
				}
				if r.Method == http.MethodDelete {
					deletes++
					return safetyResponse(200, `{}`), nil
				}
				if strings.Contains(r.URL.Path, "cachedContents") {
					return safetyResponse(200, `{"name":"cachedContents/`+safetyValue+`"}`), nil
				}
				return safetyResponse(200, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n"), nil
			})
			c := safetyConversation(t, ak.GeminiGenerateContentWire(), ak.APIKeyRotator(safetyValue), nil, ak.Rotation{})
			if err := c.AddSystem("prefix"); err != nil {
				t.Fatal(err)
			}
			sp, err := c.Savepoint()
			if err != nil {
				t.Fatal(err)
			}
			if err = safetySend(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			if operation == "release" {
				err = c.Release(sp)
			} else {
				err = c.Close()
			}
			safetyCheckChain(t, err, safetyValue)
			if deletes != 0 {
				t.Fatal("unsafe cache deletion was sent")
			}
		})
	}
}

// R-VZ8C-D8NY R-T6FE-G1JX
func TestCredentialSafetyProviderRedirects(t *testing.T) {
	wires := []func() ak.WireFormat{ak.AnthropicMessagesWire, ak.ChatWire, ak.ResponsesWire, ak.OpenAIChatWire, ak.OpenAIResponsesWire, ak.XAIChatWire, ak.XAIResponsesWire, ak.GeminiGenerateContentWire}
	for index, newWire := range wires {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			calls := 0
			safetyClient(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if strings.Contains(r.URL.String(), safetyValue) || strings.Contains(r.URL.Path, safetyValue) {
					t.Fatal("redirect request URL disclosed bearer")
				}
				response := safetyResponse(307, "")
				response.Header.Set("Location", "/"+safetyValue)
				return response, nil
			})
			var log bytes.Buffer
			c := safetyConversation(t, newWire(), ak.APIKeyRotator(safetyValue), &log, ak.Rotation{})
			err := safetySend(context.Background(), c)
			safetyCheckChain(t, err, safetyValue)
			safetyCheckLog(t, &log, safetyValue)
			if calls != 1 {
				t.Fatal("unsafe redirect was followed")
			}
		})
	}
}

// R-VZ8C-D8NY R-W0G8-R0EN
func TestCredentialSafetyGeminiCacheRedirects(t *testing.T) {
	for _, operation := range []string{"create", "release", "close"} {
		t.Run(operation, func(t *testing.T) {
			redirects := 0
			safetyClient(t, func(r *http.Request) (*http.Response, error) {
				if strings.Contains(r.URL.String(), safetyValue) || strings.Contains(r.URL.Path, safetyValue) {
					t.Fatal("cache redirect URL disclosed bearer")
				}
				if (operation == "create" && strings.Contains(r.URL.Path, "cachedContents")) || r.Method == http.MethodDelete {
					redirects++
					response := safetyResponse(307, "")
					response.Header.Set("Location", "/"+safetyValue)
					return response, nil
				}
				if strings.Contains(r.URL.Path, "cachedContents") {
					return safetyResponse(200, `{"name":"cachedContents/cache"}`), nil
				}
				return safetyResponse(200, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n"), nil
			})
			var log bytes.Buffer
			c := safetyConversation(t, ak.GeminiGenerateContentWire(), ak.APIKeyRotator(safetyValue), &log, ak.Rotation{})
			if err := c.AddSystem("prefix"); err != nil {
				t.Fatal(err)
			}
			sp, err := c.Savepoint()
			if err != nil {
				t.Fatal(err)
			}
			err = safetySend(context.Background(), c)
			if operation != "create" {
				if err != nil {
					t.Fatal(err)
				}
				if operation == "release" {
					err = c.Release(sp)
				} else {
					err = c.Close()
				}
			}
			safetyCheckChain(t, err, safetyValue)
			if operation == "create" {
				safetyCheckLog(t, &log, safetyValue)
			}
			if redirects != 1 {
				t.Fatal("unsafe cache redirect was followed")
			}
		})
	}
}

// R-VZ8C-D8NY
func TestCredentialSafetyRedirectPolicyMutations(t *testing.T) {
	for _, policy := range []string{"append-secret", "use-last-response", "rewrite-safe"} {
		t.Run(policy, func(t *testing.T) {
			calls, policyCalls := 0, 0
			safetyClient(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if strings.Contains(r.URL.String(), safetyValue) || strings.Contains(r.URL.Path, safetyValue) {
					t.Fatal("redirect policy bypassed URL safety")
				}
				if calls == 1 {
					response := safetyResponse(307, "")
					response.Header.Set("Location", "/destination")
					return response, nil
				}
				if r.URL.Path != "/safe" {
					t.Fatal("consumer redirect policy rewrite not applied")
				}
				return safetyResponse(200, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"), nil
			})
			http.DefaultClient.CheckRedirect = func(r *http.Request, _ []*http.Request) error {
				policyCalls++
				switch policy {
				case "append-secret":
					values := r.URL.Query()
					values.Set("injected", safetyValue)
					r.URL.RawQuery = values.Encode()
				case "use-last-response":
					return http.ErrUseLastResponse
				case "rewrite-safe":
					r.URL.Path = "/safe"
				}
				return nil
			}
			c := safetyConversation(t, ak.ChatWire(), ak.APIKeyRotator(safetyValue), nil, ak.Rotation{})
			err := safetySend(context.Background(), c)
			if policyCalls != 1 {
				t.Fatal("consumer redirect policy not called")
			}
			if policy == "rewrite-safe" {
				if err != nil {
					t.Fatal(err)
				}
				if calls != 2 {
					t.Fatal("consumer-approved safe redirect not followed")
				}
			} else {
				if calls != 1 {
					t.Fatal("rejected redirect followed")
				}
				safetyCheckChain(t, err, safetyValue)
				if policy == "use-last-response" {
					var e *ak.Error
					if !errors.As(err, &e) || e.Status != 307 {
						t.Fatal("consumer use-last-response policy not preserved")
					}
				}
			}
		})
	}
}
