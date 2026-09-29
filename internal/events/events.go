package events

import "time"

type Topic string

const (
	TopicServerStatus    Topic = "server:status"
	TopicConsoleLine     Topic = "console:line"
	TopicTaskChanged     Topic = "task:changed"
	TopicBackupsChanged  Topic = "backups:changed"
	TopicModsChanged     Topic = "mods:changed"
	TopicUpdateWindow    Topic = "update:window"
	TopicSchedules       Topic = "schedules:changed"
	TopicDiskStatus      Topic = "disk:status"
	TopicConfigChanged   Topic = "config:changed"
	TopicSettingsChanged Topic = "settings:changed"
	TopicStreamDesync    Topic = "stream:desync"
)

// Event is the sealed union of everything the SSE stream can carry. Events are
// invalidation signals: payloads stay minimal.
type Event interface{ topic() Topic }

func TopicOf(ev Event) Topic { return ev.topic() }

type ServerStatus struct {
	State string `json:"state" doc:"stopped|starting|running|stopping|crashed"`
}

type ConsoleLine struct {
	Seq  uint64    `json:"seq"`
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

type TaskChanged struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type BackupsChanged struct{}

type ModsChanged struct{}

type UpdateWindow struct {
	State string `json:"state"`
}

type SchedulesChanged struct{}

type DiskStatus struct {
	Level string `json:"level" doc:"worst level across monitored volumes"`
}

type ConfigChanged struct{}

type SettingsChanged struct{}

type StreamDesync struct {
	Dropped uint64 `json:"dropped"`
}

func (ServerStatus) topic() Topic     { return TopicServerStatus }
func (ConsoleLine) topic() Topic      { return TopicConsoleLine }
func (TaskChanged) topic() Topic      { return TopicTaskChanged }
func (BackupsChanged) topic() Topic   { return TopicBackupsChanged }
func (ModsChanged) topic() Topic      { return TopicModsChanged }
func (UpdateWindow) topic() Topic     { return TopicUpdateWindow }
func (SchedulesChanged) topic() Topic { return TopicSchedules }
func (DiskStatus) topic() Topic       { return TopicDiskStatus }
func (ConfigChanged) topic() Topic    { return TopicConfigChanged }
func (SettingsChanged) topic() Topic  { return TopicSettingsChanged }
func (StreamDesync) topic() Topic     { return TopicStreamDesync }

var all = []Event{
	ServerStatus{}, ConsoleLine{}, TaskChanged{}, BackupsChanged{}, ModsChanged{}, UpdateWindow{},
	SchedulesChanged{}, DiskStatus{}, ConfigChanged{}, SettingsChanged{}, StreamDesync{},
}

// EventTypeMap is handed to sse.Register and is the single source of truth the
// generated TS union is built from.
func EventTypeMap() map[string]any {
	m := make(map[string]any, len(all))
	for _, ev := range all {
		m[string(ev.topic())] = ev
	}
	return m
}

// Topics lists every topic constant, for tests.
func Topics() []Topic {
	return []Topic{TopicServerStatus, TopicConsoleLine, TopicTaskChanged, TopicBackupsChanged, TopicModsChanged,
		TopicUpdateWindow, TopicSchedules, TopicDiskStatus, TopicConfigChanged, TopicSettingsChanged, TopicStreamDesync}
}
