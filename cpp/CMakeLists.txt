cmake_minimum_required(VERSION 3.20)
project(iso8583_core VERSION 0.1.0 LANGUAGES CXX)

include(GNUInstallDirs)
include(CMakePackageConfigHelpers)

add_library(iso8583_core src/codec.cpp src/profile.cpp)
add_library(iso8583::core ALIAS iso8583_core)
set_target_properties(iso8583_core PROPERTIES EXPORT_NAME core)
target_compile_features(iso8583_core PUBLIC cxx_std_17)
target_include_directories(iso8583_core
    PUBLIC
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
        $<INSTALL_INTERFACE:${CMAKE_INSTALL_INCLUDEDIR}>)

option(ISO8583_BUILD_TESTS "Build ISO 8583 tests" ${PROJECT_IS_TOP_LEVEL})
if(ISO8583_BUILD_TESTS)
    enable_testing()
    add_executable(iso8583_tests tests/codec_test.cpp)
    target_link_libraries(iso8583_tests PRIVATE iso8583::core)
    target_compile_definitions(iso8583_tests PRIVATE
        ISO8583_EXAMPLE_PROFILE="${CMAKE_CURRENT_SOURCE_DIR}/../profiles/example-acquirer.json"
        ISO8583_CONFORMANCE_VECTORS="${CMAKE_CURRENT_SOURCE_DIR}/../testdata/conformance.vectors")
    add_test(NAME iso8583_tests COMMAND iso8583_tests)
endif()

option(ISO8583_BUILD_BENCHMARKS "Build ISO 8583 benchmarks" OFF)
if(ISO8583_BUILD_BENCHMARKS)
    add_executable(iso8583_benchmarks benchmarks/codec_benchmark.cpp)
    target_link_libraries(iso8583_benchmarks PRIVATE iso8583::core)
endif()

configure_package_config_file(
    cmake/iso8583_core-config.cmake.in
    ${CMAKE_CURRENT_BINARY_DIR}/iso8583_core-config.cmake
    INSTALL_DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/iso8583_core)
write_basic_package_version_file(
    ${CMAKE_CURRENT_BINARY_DIR}/iso8583_core-config-version.cmake
    VERSION ${PROJECT_VERSION}
    COMPATIBILITY SameMajorVersion)

install(TARGETS iso8583_core EXPORT iso8583-core-targets)
install(DIRECTORY include/ DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})
install(FILES
    ${CMAKE_CURRENT_BINARY_DIR}/iso8583_core-config.cmake
    ${CMAKE_CURRENT_BINARY_DIR}/iso8583_core-config-version.cmake
    DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/iso8583_core)
install(EXPORT iso8583-core-targets
    FILE iso8583-core-targets.cmake
    NAMESPACE iso8583::
    DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/iso8583_core)