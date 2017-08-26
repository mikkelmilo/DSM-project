package dsm_api

type DSMApiInterface interface{
	Initialize(port int) error
	Join(ip string, port int) error
	Shutdown() error
	Read(addr int) (byte, error)
	Write(addr int, val byte) error
	Malloc(size int) (int, error)
	Free(addr, size int) error
	Barrier(id uint8)
	Lock(id uint8)
	Release(id uint8)
}