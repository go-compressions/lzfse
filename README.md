# lzfse

Pure-Go implementation of Apple's LZFSE and LZVN compression formats.

## Module

```
github.com/go-compressions/lzfse
```

## API

```go
func Compress(src []byte) ([]byte, error)
func Decompress(src []byte) ([]byte, error)
```

Used by `lzfsec` and by disk image modules that need Tart layer
decompression.