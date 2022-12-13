package disabled

import (
	"github.com/ElrondNetwork/elrond-go-core/data"
	vmcommon "github.com/ElrondNetwork/elrond-vm-common"
)

// EpochNotifier is a disabled implementation of EpochNotifier interface
type EpochNotifier struct {
}

// RegisterNotifyHandler does nothing
func (en *EpochNotifier) RegisterNotifyHandler(_ vmcommon.EpochSubscriberHandler) {
}

// CurrentEpoch returns 0
func (en *EpochNotifier) CurrentEpoch() uint32 {
	return 0
}

// CheckEpoch does nothing
func (en *EpochNotifier) CheckEpoch(_ data.HeaderHandler) {
}

// IsInterfaceNil returns true if there is no value under the interface
func (en *EpochNotifier) IsInterfaceNil() bool {
	return en == nil
}
