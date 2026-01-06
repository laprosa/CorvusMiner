#pragma once

#include <string>
#include <iostream>
#include <array>
#include <vector>

// Default encryption key - will be replaced at build time
const std::string ENCRYPTION_KEY_B64 = "";

// Encrypted string placeholders - will be replaced by builder
// Format: __ENCRYPTED_0__, __ENCRYPTED_1__, etc.
const char __ENCRYPTED_0__[] = "";  // placeholder
const char __ENCRYPTED_1__[] = "";  // placeholder
const char __ENCRYPTED_2__[] = "";  // placeholder
const char __ENCRYPTED_3__[] = "";  // placeholder
const char __ENCRYPTED_4__[] = "";  // placeholder
const char __ENCRYPTED_5__[] = "";  // placeholder
const char __ENCRYPTED_6__[] = "";  // placeholder
const char __ENCRYPTED_7__[] = "";  // placeholder
const char __ENCRYPTED_8__[] = "";  // placeholder
const char __ENCRYPTED_9__[] = "";  // placeholder
const char __ENCRYPTED_10__[] = ""; // placeholder
const char __ENCRYPTED_11__[] = ""; // placeholder
const char __ENCRYPTED_12__[] = ""; // placeholder
const char __ENCRYPTED_13__[] = ""; // placeholder
const char __ENCRYPTED_14__[] = ""; // placeholder
const char __ENCRYPTED_15__[] = ""; // placeholder

// Base64 decode helper - improved version
inline std::string base64_decode_key(const std::string& encoded) {
	static const std::string base64_chars =
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

	std::string decoded;
	std::vector<int> T(256, -1);
	for (int i = 0; i < 64; i++) T[base64_chars[i]] = i;

	int val = 0, valb = -8;
	for (unsigned char c : encoded) {
		if (T[c] == -1) break;
		val = (val << 6) + T[c];
		valb += 6;
		if (valb >= 0) {
			decoded.push_back(char((val >> valb) & 0xFF));
			valb -= 8;
		}
	}
	return decoded;
}

// Runtime XOR decryption using base64-encoded key
inline std::string DecryptStringBase64(const std::string& encrypted_b64) {
	static std::string key_bytes = base64_decode_key(ENCRYPTION_KEY_B64);
	
	if (key_bytes.empty()) {
		return "";
	}
	
	// Decode the encrypted string from base64
	std::string encrypted = base64_decode_key(encrypted_b64);
	
	std::string result;
	for (size_t i = 0; i < encrypted.length(); i++) {
		result.push_back(encrypted[i] ^ key_bytes[i % key_bytes.length()]);
	}
	
	return result;
}

// Decrypt const char[] (for use with __ENCRYPTED_X__ placeholders)
inline std::string DecryptString(const char* encrypted_b64) {
	if (!encrypted_b64 || encrypted_b64[0] == '\0') return "";
	return DecryptStringBase64(std::string(encrypted_b64));
}

// ENCRYPT_STR macro - builder replaces ENCRYPT_STR("string") with DecryptString(__ENCRYPTED_N__)
// This is just a passthrough for the original source before builder processes it
#define ENCRYPT_STR(x) (x)
#define ENCRYPT_WSTR(x) (x)


