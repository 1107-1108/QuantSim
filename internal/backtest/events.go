package backtest

type EventKind string

const (
	EventBar             EventKind = "BAR"
	EventOrderAccepted   EventKind = "ORDER_ACCEPTED"
	EventOrderRejected   EventKind = "ORDER_REJECTED"
	EventFill            EventKind = "FILL"
	EventOrderExpired    EventKind = "ORDER_EXPIRED"
	EventCorporateAction EventKind = "CORPORATE_ACTION"
	EventHistoryReplay   EventKind = "HISTORY_REPLAY"
	EventEquity          EventKind = "EQUITY"
	EventFinished        EventKind = "FINISHED"
)
