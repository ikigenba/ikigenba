package agent_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/agent"
	"github.com/ikigenba/ikigenba/dory/internal/model"
	"github.com/ikigenba/ikigenba/dory/internal/render"
	"github.com/ikigenba/ikigenba/dory/internal/store"
)

const (
	supervisorPromptConstant = agent.SupervisorPrompt
	workerPromptConstant     = agent.WorkerPrompt
)

// R-IXX2-8C9Q
func TestRolesAndPromptsExposeTheFixedContract(t *testing.T) {
	t.Parallel()

	roleType := reflect.TypeOf(agent.Role(""))
	if roleType.Kind() != reflect.String {
		t.Fatalf("Role has underlying kind %v, want string", roleType.Kind())
	}

	roles := []struct {
		name string
		got  agent.Role
		want agent.Role
	}{
		{name: "supervisor", got: agent.RoleSupervisor, want: "supervisor"},
		{name: "worker", got: agent.RoleWorker, want: "worker"},
	}
	for _, tt := range roles {
		t.Run(tt.name, func(t *testing.T) {
			if reflect.TypeOf(tt.got) != roleType {
				t.Fatalf("constant type is %T, want agent.Role", tt.got)
			}
			if tt.got != tt.want {
				t.Fatalf("role is %q, want %q", tt.got, tt.want)
			}
		})
	}

	if strings.TrimSpace(supervisorPromptConstant) == "" {
		t.Error("SupervisorPrompt must contain non-whitespace instructions")
	}
	if strings.TrimSpace(workerPromptConstant) == "" {
		t.Error("WorkerPrompt must contain non-whitespace instructions")
	}
}

var _ agent.Trace = (*render.Trace)(nil)

// R-IZ4Y-M40F
func TestTraceHasTheExactDesignedMethodSet(t *testing.T) {
	t.Parallel()

	traceType := reflect.TypeOf((*agent.Trace)(nil)).Elem()
	if traceType.Kind() != reflect.Interface {
		t.Fatalf("Trace has kind %v, want interface", traceType.Kind())
	}

	want := map[string]reflect.Type{
		"Event":  reflect.TypeOf((func(string, agentkit.Event))(nil)),
		"Error":  reflect.TypeOf((func(string, error))(nil)),
		"Record": reflect.TypeOf((func(agentkit.LogRecord) string)(nil)),
	}
	if traceType.NumMethod() != len(want) {
		t.Fatalf("Trace has %d methods, want %d", traceType.NumMethod(), len(want))
	}
	for name, wantType := range want {
		method, ok := traceType.MethodByName(name)
		if !ok {
			t.Errorf("Trace is missing method %s", name)
			continue
		}
		if method.Type != wantType {
			t.Errorf("Trace.%s has type %v, want %v", name, method.Type, wantType)
		}
	}
}

// R-J0CU-ZVR4
func TestPassDataAndFunctionExposeTheExactDesignedSurface(t *testing.T) {
	t.Parallel()

	assertFields(t, reflect.TypeOf(agent.Config{}), []fieldSpec{
		{name: "Store", typ: reflect.TypeOf((*store.Store)(nil))},
		{name: "Supervisor", typ: reflect.TypeOf((*model.Factory)(nil))},
		{name: "Worker", typ: reflect.TypeOf((*model.Factory)(nil))},
		{name: "Root", typ: reflect.TypeOf("")},
		{name: "Now", typ: reflect.TypeOf((func() time.Time)(nil))},
		{name: "Trace", typ: reflect.TypeOf((*agent.Trace)(nil)).Elem()},
	})
	assertFields(t, reflect.TypeOf(agent.Result{}), []fieldSpec{
		{name: "Address", typ: reflect.TypeOf("")},
		{name: "Report", typ: reflect.TypeOf("")},
		{name: "Usage", typ: reflect.TypeOf(agentkit.Usage{})},
		{name: "Cost", typ: reflect.TypeOf(agentkit.Cost(0))},
	})

	wantRunPassType := reflect.TypeOf((func(context.Context, agent.Config, string) (agent.Result, error))(nil))
	if got := reflect.TypeOf(agent.RunPass); got != wantRunPassType {
		t.Errorf("RunPass has type %v, want %v", got, wantRunPassType)
	}
}

type fieldSpec struct {
	name string
	typ  reflect.Type
}

func assertFields(t *testing.T, got reflect.Type, want []fieldSpec) {
	t.Helper()

	if got.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", got.Name(), got.NumField(), len(want))
	}
	for i, wantField := range want {
		gotField := got.Field(i)
		if gotField.Name != wantField.name || gotField.Type != wantField.typ {
			t.Errorf("%s field %d is %s %v, want %s %v", got.Name(), i, gotField.Name, gotField.Type, wantField.name, wantField.typ)
		}
	}
}
