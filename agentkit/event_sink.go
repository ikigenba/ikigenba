package agentkit

type eventRecordKind uint8

const (
	eventRecordMessage eventRecordKind = iota + 1
	eventRecordToolUse
	eventRecordToolResult
	eventRecordOutput
)

// eventRecord is the private, message-granular bridge consumed by the later
// durable-log implementation. It deliberately contains no turn metadata.
type eventRecord struct {
	kind  eventRecordKind
	value any
}

type eventSink interface {
	record(eventRecord)
}

func recordForEvent(event Event) eventRecord {
	switch value := event.(type) {
	case MessageDone:
		return eventRecord{kind: eventRecordMessage, value: value.Message}
	case ToolCall:
		return eventRecord{kind: eventRecordToolUse, value: value.Use}
	case ToolReturn:
		return eventRecord{kind: eventRecordToolResult, value: value.Result}
	case OutputDone:
		return eventRecord{kind: eventRecordOutput, value: value.Value}
	default:
		panic("agentkit: invalid Event implementation")
	}
}

func publishEvent(sink eventSink, yield func(Event) bool, event Event) bool {
	if sink != nil {
		sink.record(recordForEvent(event))
	}
	return yield(event)
}

// recordMessage writes one message record directly to sink for a Message
// that has no corresponding live Stream event — the turn's opening RoleUser
// message (D15, R-TBAJ-ZUWZ), the aggregated RoleTool round-trip message
// (R-TCIG-DMNO), and an AddSystem append (R-TX8Q-VQ9H).
func recordMessage(sink eventSink, message Message) {
	if sink != nil {
		sink.record(eventRecord{kind: eventRecordMessage, value: message})
	}
}
