package domain

// FileWitness identifies the exact flushed file object prepared for a rename.
// It is private recovery data and is not part of any UI response.
type FileWitness struct {
	VolumeSerial uint64   `json:"volumeSerial"`
	FileID       [16]byte `json:"fileId"`
	CreationTime uint64   `json:"creationTime"`
	TempName     string   `json:"tempName"`
}
