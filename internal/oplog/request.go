package oplog

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// Request is the mutable activity state for one HTTP request. The activity
// middleware stores it in the request context; auth fills in the actor, and
// handlers may describe or suppress the request. Domain events emitted while
// the request runs inherit its id, IP and actor so they correlate.
type Request struct {
	mu sync.Mutex

	ID        string
	IP        string
	Method    string
	Path      string
	UserAgent string

	actorID    *uuid.UUID
	actorName  string
	authMethod string

	category string
	action   string
	message  string
	details  map[string]any
	suppress bool
	force    bool
	audited  bool
	failure  string
}

type requestKey struct{}

// WithRequest attaches activity state to ctx.
func WithRequest(ctx context.Context, r *Request) context.Context {
	return context.WithValue(ctx, requestKey{}, r)
}

// RequestFrom returns the activity state on ctx, or nil.
func RequestFrom(ctx context.Context) *Request {
	if ctx == nil {
		return nil
	}
	r, _ := ctx.Value(requestKey{}).(*Request)
	return r
}

// SetActor records who made the request. method is "session", "api_key",
// "token" or similar.
func (r *Request) SetActor(id uuid.UUID, name, method string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if id != uuid.Nil {
		v := id
		r.actorID = &v
	}
	if name != "" {
		r.actorName = name
	}
	if method != "" {
		r.authMethod = method
	}
}

// SetActorName records a name without an id (for example the username tried
// on a failed sign-in).
func (r *Request) SetActorName(name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.actorName = name
	r.mu.Unlock()
}

// Actor returns the recorded actor.
func (r *Request) Actor() (*uuid.UUID, string, string) {
	if r == nil {
		return nil, "", ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actorID, r.actorName, r.authMethod
}

// Describe overrides how the request is summarised. Empty values keep the
// default derived from the route.
func (r *Request) Describe(category, action, message string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if category != "" {
		r.category = category
	}
	if action != "" {
		r.action = action
	}
	if message != "" {
		r.message = message
	}
}

// Description returns overrides set with Describe.
func (r *Request) Description() (category, action, message string) {
	if r == nil {
		return "", "", ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.category, r.action, r.message
}

// Annotate adds a non-secret detail to the request entry.
func (r *Request) Annotate(key string, value any) {
	if r == nil || key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.details == nil {
		r.details = map[string]any{}
	}
	r.details[key] = value
}

// Details returns a copy of annotations.
func (r *Request) Details() map[string]any {
	out := map[string]any{}
	if r == nil {
		return out
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, v := range r.details {
		out[k] = v
	}
	return out
}

// Suppress drops the request entry when it succeeds (high-volume, low-value
// calls such as volume nudges). Failures are still recorded.
func (r *Request) Suppress() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.suppress = true
	r.mu.Unlock()
}

// Force records the request even when it would normally be skipped.
func (r *Request) Force() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.force = true
	r.mu.Unlock()
}

// Flags reports Suppress/Force.
func (r *Request) Flags() (suppress, force bool) {
	if r == nil {
		return false, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.suppress, r.force
}

// Fail marks the request as failed even though it returned a non-error
// status (for example an OAuth callback that redirects with an error).
func (r *Request) Fail(reason string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if reason == "" {
		reason = "failed"
	}
	r.failure = reason
	r.force = true
	r.mu.Unlock()
}

// Failure returns the reason passed to Fail.
func (r *Request) Failure() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failure
}

// MarkAudited notes that an explicit audit event was written for this request.
func (r *Request) MarkAudited() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.audited = true
	r.mu.Unlock()
}

// Audited reports whether MarkAudited was called.
func (r *Request) Audited() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.audited
}

func fillFromContext(ctx context.Context, e Entry) Entry {
	r := RequestFrom(ctx)
	if r == nil {
		return e
	}
	if e.RequestID == "" {
		e.RequestID = r.ID
	}
	if e.IP == "" {
		e.IP = r.IP
	}
	id, name, _ := r.Actor()
	if e.ActorID == nil && id != nil {
		e.ActorID = id
	}
	if e.ActorName == "" {
		e.ActorName = name
	}
	return e
}
