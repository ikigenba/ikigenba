package repos

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestProtocolAdmissionOrdersLabelsAndAssertsIdentity(t *testing.T) {
	// R-FDAF-MVIC
	var names []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Error(err)
		}
		if r.Header.Get("X-Owner-Email") != "owner@example.com" || r.Header.Get("X-Client-Id") != "repos:session-7" {
			t.Errorf("identity headers = %q, %q", r.Header.Get("X-Owner-Email"), r.Header.Get("X-Client-Id"))
		}
		names = append(names, call.Params.Name)
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()
	issue := 7
	protocol := NewProtocol(NewGitHubPeerAt(server.URL, server.Client()))
	if err := protocol.Admit(context.Background(), Session{
		ID: "session-7", RepoName: "alpha", OwnerEmail: "owner@example.com", IssueNumber: &issue, Attempt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"label_remove", "label_add", "issue_comment"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("calls = %v, want %v", names, want)
	}
}

func TestProtocolRetryAdmissionRemovesFailedLabel(t *testing.T) {
	// R-FKLT-XHYI
	var names []string
	var labels []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call struct {
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&call)
		names = append(names, call.Params.Name)
		if label, ok := call.Params.Arguments["label"].(string); ok {
			labels = append(labels, label)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()
	issue := 9
	protocol := NewProtocol(NewGitHubPeerAt(server.URL, server.Client()))
	if err := protocol.Admit(context.Background(), Session{
		ID: "retry", RepoName: "alpha", OwnerEmail: "owner@example.com", IssueNumber: &issue, Attempt: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"label_remove", "label_remove", "label_add", "issue_comment"}) ||
		!reflect.DeepEqual(labels, []string{"execute", "failed"}) {
		t.Fatalf("retry calls = %v, labels = %v", names, labels)
	}
}

func TestProtocolFetchIssueDecodesCommentListEnvelope(t *testing.T) {
	// R-894D-CUA2
	tests := []struct {
		name     string
		comments any
		want     IssueContent
		wantErr  bool
	}{
		{
			name: "wrapped comments preserve order",
			comments: map[string]any{"items": []any{
				map[string]any{"body": "First comment"},
				map[string]any{"body": "Second comment"},
			}},
			want: IssueContent{
				Title:    "Envelope issue",
				Body:     "Read the whole thread.",
				Comments: []string{"First comment", "Second comment"},
			},
		},
		{
			name: "bare comment array is rejected",
			comments: []any{
				map[string]any{"body": "First comment"},
				map[string]any{"body": "Second comment"},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var call struct {
					Params struct {
						Name string `json:"name"`
					} `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
					t.Error(err)
				}
				var result any
				switch call.Params.Name {
				case "issue_get":
					result = map[string]any{
						"number": 42,
						"title":  "Envelope issue",
						"body":   "Read the whole thread.",
					}
				case "issue_comments":
					result = test.comments
				default:
					t.Errorf("unexpected github call %q", call.Params.Name)
					result = map[string]any{}
				}
				if err := json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"result":  result,
				}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()

			issueNumber := 42
			protocol := NewProtocol(NewGitHubPeerAt(server.URL, server.Client()))
			got, err := protocol.FetchIssue(context.Background(), Session{
				ID:          "session-42",
				RepoName:    "alpha",
				OwnerEmail:  "owner@example.com",
				IssueNumber: &issueNumber,
			})
			if test.wantErr {
				if err == nil {
					t.Fatal("FetchIssue accepted a bare issue_comments array")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("FetchIssue() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestProtocolSuccessRequiresCreatedPRHTMLURL(t *testing.T) {
	// R-APSC-24AL
	tests := []struct {
		name       string
		createdPR  map[string]any
		wantURL    string
		wantErr    bool
		wantBodies []string
	}{
		{
			name:       "html_url is persisted and posted",
			createdPR:  map[string]any{"number": 17, "html_url": "https://example.test/pull/17"},
			wantURL:    "https://example.test/pull/17",
			wantBodies: []string{"https://example.test/pull/17"},
		},
		{
			name:      "legacy url field fails loud",
			createdPR: map[string]any{"number": 17, "url": "https://example.test/pull/17"},
			wantErr:   true,
		},
		{
			name:      "empty html_url fails loud",
			createdPR: map[string]any{"number": 17, "html_url": ""},
			wantErr:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var commentBodies []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var call struct {
					Params struct {
						Name      string         `json:"name"`
						Arguments map[string]any `json:"arguments"`
					} `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
					t.Error(err)
				}

				result := any(map[string]any{})
				switch call.Params.Name {
				case "pr_create":
					result = test.createdPR
				case "issue_comment":
					body, ok := call.Params.Arguments["body"].(string)
					if !ok {
						t.Errorf("issue_comment body = %#v", call.Params.Arguments["body"])
					} else {
						commentBodies = append(commentBodies, body)
					}
				case "label_remove":
				default:
					t.Errorf("unexpected github call %q", call.Params.Name)
				}
				if err := json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"result":  result,
				}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()

			issueNumber := 17
			protocol := NewProtocol(NewGitHubPeerAt(server.URL, server.Client()))
			gotURL, err := protocol.Success(context.Background(), Session{
				ID:          "session-17",
				RepoName:    "alpha",
				OwnerEmail:  "owner@example.com",
				IssueNumber: &issueNumber,
				Branch:      "ikigenba/issue-17",
			}, Repo{
				Name:          "alpha",
				DefaultBranch: "main",
			}, "Fix the issue", "Implemented the fix.", "passed")

			if test.wantErr {
				if err == nil {
					t.Fatal("Success accepted a created PR without html_url")
				}
				if gotURL != "" {
					t.Fatalf("Success URL = %q, want empty on error", gotURL)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if gotURL != test.wantURL {
					t.Fatalf("Success URL = %q, want %q", gotURL, test.wantURL)
				}
			}
			if !reflect.DeepEqual(commentBodies, test.wantBodies) {
				t.Fatalf("issue_comment bodies = %#v, want %#v", commentBodies, test.wantBodies)
			}
		})
	}
}
