package agentkit

type eventRecordKind uint8

const (
	eventRecordMessage eventRecordKind = iota + 1
	eventRecordToolUse
	eventRecordToolResult
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
