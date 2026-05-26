package trajectory

import "context"

type Sink interface {
	Write(ctx context.Context, event Event) error
	Close() error
}
