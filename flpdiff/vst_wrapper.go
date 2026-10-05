package flpdiff

const (
	vstEventIDName       = 54
	vstEventIDVendor     = 56
	vstEventIDPluginPath = 55
	vstEventIDFourCC     = 51
	vstRecordHeaderBytes = 12
)

// VSTWrapperInfo is the decoded VST-wrapper record stream.
type VSTWrapperInfo struct {
	Name       *string
	Vendor     *string
	PluginPath *string
	FourCC     *string
	Marker     *int
}

// decodeVSTWrapper parses a 0xD5 VST-wrapper payload. Defensive: never panics on malformed input.
func decodeVSTWrapper(payload []byte) VSTWrapperInfo {
	info := VSTWrapperInfo{}
	if len(payload) < 4 {
		return info
	}
	info.Marker = Ptr(int(payload[0]))
	pos := 4
	for pos+vstRecordHeaderBytes <= len(payload) {
		id := le.Uint32(payload[pos:])
		len64 := le.Uint64(payload[pos+4:])
		if len64 > uint64(len(payload)-pos-vstRecordHeaderBytes) {
			break
		}
		n := int(len64)
		dataStart := pos + vstRecordHeaderBytes
		data := payload[dataStart : dataStart+n]
		switch id {
		case vstEventIDName:
			info.Name = Ptr(decodeUTF8Lossy(data))
		case vstEventIDVendor:
			info.Vendor = Ptr(decodeUTF8Lossy(data))
		case vstEventIDPluginPath:
			info.PluginPath = Ptr(decodeUTF8Lossy(data))
		case vstEventIDFourCC:
			info.FourCC = Ptr(decodeUTF8Lossy(data))
		}
		pos = dataStart + n
	}
	return info
}
