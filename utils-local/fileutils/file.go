package fileutils

import (
	"bufio"
	"bytes"
	"fmt"
	en "github.com/chainreactors/utils/encode"
	"os"
	"sync"
)

// 预定义的常用文件打开模式
const (
	// ModeCreate 创建模式：如果文件存在则报错，不存在则创建
	ModeCreate = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	// ModeOverwrite 覆盖模式：如果文件存在则覆盖，不存在则创建
	ModeOverwrite = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	// ModeAppend 追加模式：如果文件存在则追加，不存在则创建
	ModeAppend = os.O_WRONLY | os.O_CREATE | os.O_APPEND
)

// NewFile 创建一个新的文件写入器
// filename: 文件名
// mode: 写入模式 (使用 os.O_* 标志位，或预定义的 ModeCreate, ModeOverwrite, ModeAppend)
// encode: 是否启用编码
// lazy: 是否延迟初始化
func NewFile(filename string, mode int, encode, lazy bool) (*File, error) {
	file := &File{
		filename:    filename,
		mode:        mode,
		encode:      encode,
		lazy:        lazy,
		buf:         bytes.NewBuffer([]byte{}),
		bufferSize:  4096,
		initialized: false,
		closed:      false,
		Handler: func(s string) string {
			return s
		},
		Encoder: en.MustDeflateDeCompress,
	}

	if !lazy {
		if err := file.init(); err != nil {
			return nil, fmt.Errorf("failed to initialize file %s: %w", filename, err)
		}
	}

	return file, nil
}

// NewFileWithOptions 使用选项创建文件写入器
func NewFileWithOptions(filename string, opts *FileOptions) (*File, error) {
	if opts == nil {
		opts = DefaultFileOptions()
	}

	file, err := NewFile(filename, opts.Mode, opts.Encode, opts.Lazy)
	if err != nil {
		return nil, err
	}
	file.bufferSize = opts.BufferSize
	file.Handler = opts.Handler
	file.ClosedAppend = opts.ClosedAppend
	file.Encoder = opts.Encoder

	return file, nil
}

// FileOptions 文件选项配置
type FileOptions struct {
	Mode         int // 使用 os.O_* 标志位
	Encode       bool
	Lazy         bool
	BufferSize   int
	Handler      func(string) string
	Encoder      func([]byte) []byte
	ClosedAppend string
}

// DefaultFileOptions 返回默认的文件选项
func DefaultFileOptions() *FileOptions {
	return &FileOptions{
		Mode:         ModeAppend,
		Encode:       false,
		Lazy:         false,
		BufferSize:   4096,
		ClosedAppend: "",
		Handler: func(s string) string {
			return s
		},
		Encoder: en.MustDeflateDeCompress,
	}
}

// File 线程安全的文件写入器
type File struct {
	filename    string
	mode        int // 使用 os.O_* 标志位
	encode      bool
	lazy        bool
	bufferSize  int
	initialized bool
	closed      bool

	fileHandler *os.File
	fileWriter  *bufio.Writer
	buf         *bytes.Buffer
	mutex       sync.RWMutex

	Handler      func(string) string
	Encoder      func([]byte) []byte
	ClosedAppend string
}

// init 初始化文件写入器（内部方法）
func (f *File) init() error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if f.initialized {
		return nil
	}

	var err error
	f.fileHandler, err = os.OpenFile(f.filename, f.mode, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s with mode %d: %w", f.filename, f.mode, err)
	}

	f.fileWriter = bufio.NewWriter(f.fileHandler)
	f.initialized = true
	return nil
}

// Write 实现 io.Writer 接口，线程安全地写入数据
// 在最底层实现 Encoder 功能
func (f *File) Write(p []byte) (n int, err error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if f.closed {
		return 0, fmt.Errorf("file %s is closed", f.filename)
	}

	if !f.initialized {
		if err := f.init(); err != nil {
			return 0, err
		}
	}

	n, err = f.buf.Write(p)
	if err != nil {
		return n, err
	}

	if f.buf.Len() >= f.bufferSize {
		if err := f.flush(); err != nil {
			return n, err
		}
	}

	return len(p), nil // 返回原始数据长度
}

// WriteString 线程安全地写入字符串
func (f *File) WriteString(s string) (n int, err error) {
	if f.Handler != nil {
		s = f.Handler(s)
	}
	return f.Write([]byte(s))
}

// WriteLine 写入一行数据（自动添加换行符）
func (f *File) WriteLine(s string) error {
	_, err := f.WriteString(s + "\n")
	return err
}

// WriteBytes 写入字节数据
func (f *File) WriteBytes(bs []byte) error {
	_, err := f.Write(bs)
	return err
}

// SyncWrite 同步写入（写入后立即刷新到磁盘）
func (f *File) SyncWrite(s string) error {
	if _, err := f.WriteString(s); err != nil {
		return err
	}
	return f.Sync()
}

// Sync 将缓冲区数据刷新到磁盘
func (f *File) Sync() error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	return f.flush()
}

// flush 内部刷新方法（需要在持有锁的情况下调用）
func (f *File) flush() error {
	if f.fileHandler == nil || f.buf.Len() == 0 {
		return nil
	}

	var data []byte
	if f.encode && f.Encoder != nil {
		data = f.Encoder(f.buf.Bytes())
	} else {
		data = f.buf.Bytes()
	}

	// 直接写入缓冲区数据（编码已在 Write 方法中处理）
	if _, err := f.fileWriter.Write(data); err != nil {
		return fmt.Errorf("failed to write to file %s: %w", f.filename, err)
	}

	f.buf.Reset()

	if err := f.fileWriter.Flush(); err != nil {
		return fmt.Errorf("failed to flush file %s: %w", f.filename, err)
	}

	return nil
}

// Close 关闭文件写入器
// ClosedAppend 在此方法中生效
func (f *File) Close() error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if f.closed {
		return nil
	}

	// 在关闭前写入 ClosedAppend 内容
	if f.ClosedAppend != "" {
		if _, err := f.buf.WriteString(f.ClosedAppend); err != nil {
			return fmt.Errorf("failed to write ClosedAppend: %w", err)
		}
	}

	// 刷新剩余数据
	if err := f.flush(); err != nil {
		return err
	}

	// 关闭文件
	if f.fileHandler != nil {
		if err := f.fileHandler.Close(); err != nil {
			return fmt.Errorf("failed to close file %s: %w", f.filename, err)
		}
	}

	f.closed = true
	return nil
}

// IsInitialized 检查文件是否已初始化
func (f *File) IsInitialized() bool {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	return f.initialized
}

// IsClosed 检查文件是否已关闭
func (f *File) IsClosed() bool {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	return f.closed
}

// GetFilename 获取文件名
func (f *File) GetFilename() string {
	return f.filename
}

// GetMode 获取写入模式
func (f *File) GetMode() int {
	return f.mode
}

// BufferLen 获取当前缓冲区大小
func (f *File) BufferLen() int {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	return f.buf.Len()
}

// SetBufferSize 设置缓冲区大小阈值
func (f *File) SetBufferSize(size int) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if size > 0 {
		f.bufferSize = size
	}
}

// SetHandler 设置字符串处理函数
func (f *File) SetHandler(handler func(string) string) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if handler != nil {
		f.Handler = handler
	}
}

// SetEncoder 设置编码函数
func (f *File) SetEncoder(encoder func([]byte) []byte) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if encoder != nil {
		f.Encoder = encoder
	}
}

// EnableEncoding 启用或禁用编码
func (f *File) EnableEncoding(enable bool) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.encode = enable
}

// SetClosedAppend 设置关闭时追加的内容
func (f *File) SetClosedAppend(content string) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.ClosedAppend = content
}

// GetClosedAppend 获取关闭时追加的内容
func (f *File) GetClosedAppend() string {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	return f.ClosedAppend
}

// GetModeString 获取模式的字符串描述
func GetModeString(mode int) string {
	switch mode {
	case ModeCreate:
		return "create"
	case ModeOverwrite:
		return "overwrite"
	case ModeAppend:
		return "append"
	default:
		return fmt.Sprintf("custom(%d)", mode)
	}
}
