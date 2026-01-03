# CorvusMiner Builder

A Golang-based build system for CorvusMiner Client that uses interactive terminal prompts and automatic string encryption.

## Features

- **Cross-platform**: Works on Windows, Linux, and macOS
- **Interactive mode**: Prompts for build options via terminal
- **Automatic string encryption**: Encrypts all strings wrapped in `ENCRYPT_STR()` and `ENCRYPT_WSTR()` macros
- **Unique XOR keys**: Generates a unique 256-bit XOR key for each build
- **Base64 encoding**: Encrypted strings are stored as base64 for portability
- **Runtime decryption**: Strings are decrypted at runtime using the injected key
- **Automatic tool verification**: Checks for required build tools
- **Clean builds**: Optional build directory cleaning
- **Multi-core compilation**: Automatic parallelization

## Building the Builder

```bash
go build -o builder main.go
```

## Usage

Simply run the builder:

```bash
./builder
```

The builder will prompt you for:

1. **Mining pool URL** - Enter your mining pool URL or press Enter to skip
2. **Debug mode** (Windows only) - Choose whether to show the console window
3. **Clean build** - Choose whether to clean the build directory before building

## Encryption Process

The builder automatically:

1. Generates a unique random 256-bit XOR key
2. Base64 encodes the key and injects it into the encryption header
3. Scans all `.cpp` and `.h` files in the Client directory
4. Finds all `ENCRYPT_STR("...")` and `ENCRYPT_WSTR(L"...")` calls
5. Encrypts each string using XOR with the generated key
6. Base64 encodes the encrypted strings
7. Replaces the macro calls with `DecryptStringBase64()` calls containing the encrypted strings
8. During build compilation, the encrypted strings are decrypted at runtime using the injected key
9. After the build completes, all source files are restored to their original state

This ensures each binary build has a unique encryption key, making it much harder to extract sensitive strings from the executable.

## Build Process

1. Backs up all source files
2. Generates unique XOR encryption key
3. Injects key into encryption header
4. Encrypts all ENCRYPT_STR() and ENCRYPT_WSTR() macro calls
5. Toggles WIN32 flag for debug mode (Windows only)
6. Runs CMake configuration
7. Compiles with Make
8. Restores all source files to original state
9. Verifies output executable

## Requirements

### Windows
- CMake (portable version in `portable_tools/cmake`)
- Make (portable version in `portable_tools/make`)
- MinGW-w64 (portable version in `portable_tools/mingw64`)

### Linux/macOS
- cmake
- make
- C++ compiler (gcc/clang)

## Output

On success, the built executable is located at:
- Windows: `Client/build/corvus.exe`
- Linux/macOS: `Client/build/corvus`

## String Encryption Example

Before build:
```cpp
std::string url = ENCRYPT_STR("http://192.168.1.81:8080/api/miners/submit");
```

During compilation (temporary):
```cpp
std::string url = DecryptStringBase64("qW259RWUI7Uq2hjrwDck...");
```

At runtime, the encrypted string is decrypted using the injected key stored in `ENCRYPTION_KEY_B64`.
