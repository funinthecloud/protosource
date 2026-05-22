package responsev1

import (
	"google.golang.org/protobuf/proto"
)

type Responseer interface {
	proto.Message
	GetId() string
	GetVersion() int64
}