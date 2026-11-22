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

	allocate := ole32.NewProc("CoTaskMemAlloc")
	var enumerated bool
	enumerator := &iEnumString{VTable: &iEnumStringVTable{
		Next: syscall.NewCallback(func(_ uintptr, _ uintptr, nameOut uintptr, fetchedOut uintptr) uintptr {
			if enumerated {
				return callResult(SFalse)
			}
			enumerated = true
			name, _, _ := allocate.Call(4) // One UTF-16 character and its terminator.
			if name == 0 {
				return 0x8007000E // E_OUTOFMEMORY
			}
			*(*[2]uint16)(comPointer(name)) = [2]uint16{'B', 0}
			*(*uintptr)(comPointer(nameOut)) = name
			*(*uint32)(comPointer(fetchedOut)) = 1
			return sOK
		}),
	}}
	var pins runtime.Pinner
	pins.Pin(enumerator)
	pins.Pin(enumerator.VTable)
	defer pins.Unpin()

	var itemIDResult HRESULT
	browse := &iopcBrowseServerAddressSpace{VTable: &iopcBrowseServerAddressSpaceVTable{
		QueryOrganization: syscall.NewCallback(func(_ uintptr, organizationOut uintptr) uintptr {
			*(*uint32)(comPointer(organizationOut)) = opcNamespaceHierarchical
			return sOK
		}),
		ChangeBrowsePosition: syscall.NewCallback(func(_, _, _ uintptr) uintptr { return sOK }),
		BrowseOPCItemIDs: syscall.NewCallback(func(_, _, _, _, _ uintptr, enumeratorOut uintptr) uintptr {
			*(**iEnumString)(comPointer(enumeratorOut)) = enumerator
			return sOK
		}),
		GetItemID: syscall.NewCallback(func(_, _, _ uintptr) uintptr { return callResult(itemIDResult) }),
	}}
	session := &daThreadSession{browse: browse}
	request := BrowseRequest{Filter: BrowseFilterBranch}

	itemIDResult = hresultFromCall(eInvalidArg)
	result, err := session.browseAddressSpace(request, DefaultLimits())
	if err != nil || len(result.Entries) != 1 || result.Entries[0].Name != "B" || result.Entries[0].ItemID != nil {
		t.Fatalf("branch naming refusal returned %+v, %v", result, err)
	}

	enumerated = false
	itemIDResult = rpcEDisconnected
	result, err = session.browseAddressSpace(request, DefaultLimits())
	source, ok := AsSourceError(err)
	if !ok || source.HRESULT != rpcEDisconnected || source.Operation != "IOPCBrowseServerAddressSpace::GetItemID" ||
		len(result.Entries) != 0 {
		t.Fatalf("branch connection loss returned %+v, %v", result, err)
	}
}
