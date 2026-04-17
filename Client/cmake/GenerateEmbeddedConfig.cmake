# Generate C++ header with embedded config JSON as hex-encoded bytes
# Usage: generate_embedded_config(INPUT_FILE OUTPUT_FILE)

function(generate_embedded_config INPUT_FILE OUTPUT_FILE)
    message(STATUS "Generating embedded config from ${INPUT_FILE}...")
    
    # Check if input file exists
    if(NOT EXISTS "${INPUT_FILE}")
        message(FATAL_ERROR "Embedded config file not found: ${INPUT_FILE}")
    endif()
    
    # Create output directory if it doesn't exist
    get_filename_component(OUTPUT_DIR "${OUTPUT_FILE}" DIRECTORY)
    file(MAKE_DIRECTORY "${OUTPUT_DIR}")
    
    # Read the JSON file as raw hex to avoid encoding issues
    file(READ "${INPUT_FILE}" CONFIG_JSON HEX)
    
    # Split hex string into pairs
    string(REGEX REPLACE "([0-9a-f][0-9a-f])" "\\1;" HEX_LIST "${CONFIG_JSON}")
    
    # Build the byte array source code
    set(HEX_BYTES "")
    set(BYTE_COUNT 0)
    foreach(HEX_BYTE ${HEX_LIST})
        if(HEX_BYTE AND NOT HEX_BYTE STREQUAL ";")
            string(APPEND HEX_BYTES "0x${HEX_BYTE}, ")
            math(EXPR BYTE_COUNT "${BYTE_COUNT} + 1")
            if(BYTE_COUNT EQUAL 16)
                string(APPEND HEX_BYTES "\n        ")
                set(BYTE_COUNT 0)
            endif()
        endif()
    endforeach()
    
    # Remove trailing comma and whitespace
    string(REGEX REPLACE ", \n        $" "" HEX_BYTES "${HEX_BYTES}")
    string(REGEX REPLACE ", $" "" HEX_BYTES "${HEX_BYTES}")
    
    # Create template with placeholder for hex bytes
    set(TEMPLATE_CONTENT 
"#pragma once

#include <string>
#include \"encryption.h\"

// Embedded configuration JSON (generated from embedded_config.json)
// Stored as hex-encoded bytes to preserve exact binary content
inline std::string GetEmbeddedConfigJson() {
    static const unsigned char config_bytes[] = {
        @HEX_BYTES@
    };
    return std::string(reinterpret_cast<const char*>(config_bytes), sizeof(config_bytes));
}

static const std::string EMBEDDED_CONFIG_JSON = GetEmbeddedConfigJson();
")
    
    # Write template file for configure_file
    set(TEMP_TEMPLATE "${OUTPUT_DIR}/config.h.in")
    file(WRITE "${TEMP_TEMPLATE}" "${TEMPLATE_CONTENT}")
    
    # Use configure_file to substitute variables
    configure_file("${TEMP_TEMPLATE}" "${OUTPUT_FILE}" @ONLY)
    
    message(STATUS "Generated embedded config header with hex encoding: ${OUTPUT_FILE}")
endfunction()

