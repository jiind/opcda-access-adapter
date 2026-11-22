//go:build windows

package opcda

import (
	"runtime"
	"syscall"
	"testing"
)

func TestBranchBrowseDistinguishesRefusalFromConnectionLoss(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	itemIDResult := hresultFromCall(eInvalidArg)
	browse := &iopcBrowseServerAddressSpace{VTable: &iopcBrowseServerAddressSpaceVTable{
		GetItemID: syscall.NewCallback(func(_, _, _ uintptr) uintptr { return callResult(itemIDResult) }),
	}}
	names := []string{"B"}

	entries, err := browse.branchEntries(names, DefaultLimits())
	if err != nil || len(entries) != 1 || entries[0].Name != "B" || entries[0].ItemID != nil {
		t.Fatalf("branch naming refusal returned %+v, %v", entries, err)
	}

	itemIDResult = rpcEDisconnected
	entries, err = browse.branchEntries(names, DefaultLimits())
	source, ok := AsSourceError(err)
	if !ok || source.HRESULT != rpcEDisconnected || source.Operation != "IOPCBrowseServerAddressSpace::GetItemID" ||
		len(entries) != 0 {
		t.Fatalf("branch connection loss returned %+v, %v", entries, err)
	}
}
