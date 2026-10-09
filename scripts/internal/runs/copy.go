package runs

// Admission and delivery refusal copy.
const (
	Stopping  string = "scripts is stopping; try again later"
	Starting  string = "a run for this event is starting; try again later"
	NoEventID string = "event has no id"
	NoRuns    string = "runs are unavailable: %s"
	QueueFull string = "the run queue is full (%d queued); try again later"
)
