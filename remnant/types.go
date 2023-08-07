package remnant

type PersistenceInfo struct {
	// struct FInfo
	// {
	// 	uint64 UniqueID = 0;
	// 	uint32 Offset = 0;
	// 	uint32 Length = 0;
	// };
	UniqueID uint64
	Offset   uint32
	Length   uint32
}

type PersistenceContainerHeader struct {
	// struct FHeader
	// {
	// 	uint32 Version = 0;
	// 	uint32 IndexOffset = 0;
	// 	uint32 DynamicOffset = 0;
	// };
	Version       uint32
	IndexOffset   uint32
	DynamicOffset uint32
}

type PersistenceContainer struct {
	Header PersistenceContainerHeader
	Info   []PersistenceInfo
}
