// Package developer implements bounded, explicitly workspace-scoped file tools.
// Apply is an effect primitive; the application must durably consume a per-action
// approval for the complete immutable Change before calling it.
package developer

const MaxFileBytes = 512 * 1024
const MaxFiles = 1000
const MaxDepth = 12
const MaxTotalBytes = 8 * 1024 * 1024
const MaxMatches = 200

type Error string

func (e Error) Error() string { return string(e) }
func (e Error) Code() string  { return string(e) }

type Options struct {
	Exclusions []string `json:"exclusions"`
	MaxFiles   int      `json:"maxFiles"`
	MaxDepth   int      `json:"maxDepth"`
	MaxBytes   int      `json:"maxBytes"`
}
type Entry struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Language string `json:"language"`
	Bytes    int64  `json:"bytes"`
}
type File struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Hash     string `json:"hash"`
	Language string `json:"language"`
	Type     string `json:"type"`
	Identity string `json:"identity"`
	Bytes    int64  `json:"bytes"`
}
type Language struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
}
type Dependency struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Manifest string `json:"manifest"`
	Kind     string `json:"kind"`
}
type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}
type Symbol struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Kind string `json:"kind"`
}
type Call struct {
	Caller string `json:"caller"`
	Callee string `json:"callee"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
}
type Report struct {
	BytesRead    int64        `json:"bytesRead"`
	FilesVisited int          `json:"filesVisited"`
	Root         string       `json:"root"`
	Entries      []Entry      `json:"entries"`
	Languages    []Language   `json:"languages"`
	Dependencies []Dependency `json:"dependencies"`
	Matches      []Match      `json:"matches"`
	Symbols      []Symbol     `json:"symbols"`
	Calls        []Call       `json:"calls"`
	Truncated    bool         `json:"truncated"`
	Warnings     []string     `json:"warnings"`
}
type Change struct {
	Root           string   `json:"root"`
	RelativePath   string   `json:"relativePath"`
	Operation      string   `json:"operation"`
	BeforeContent  string   `json:"beforeContent"`
	AfterContent   string   `json:"afterContent"`
	BeforeHash     string   `json:"beforeHash"`
	AfterHash      string   `json:"afterHash"`
	Diff           string   `json:"diff"`
	RootIdentity   string   `json:"rootIdentity"`
	ParentIdentity string   `json:"parentIdentity"`
	BeforeIdentity string   `json:"beforeIdentity"`
	RecoveryPath   string   `json:"recoveryPath"`
	Exclusions     []string `json:"exclusions,omitempty"`
}
type ChangeResult struct {
	Applied      bool   `json:"applied"`
	Verified     bool   `json:"verified"`
	RelativePath string `json:"relativePath"`
	AfterHash    string `json:"afterHash"`
	Identity     string `json:"identity"`
	RecoveryPath string `json:"recoveryPath"`
	Atomic       bool   `json:"atomic"`
}
