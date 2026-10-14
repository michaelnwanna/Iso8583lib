cmake_minimum_required(VERSION 3.20)
project(iso8583_core VERSION 0.1.0 LANGUAGES CXX)

include(GNUInstallDirs)

add_library(iso8583_core src/codec.cpp)
add_library(iso8583::core ALIAS iso8583_core)
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
    add_test(NAME iso8583_tests COMMAND iso8583_tests)
endif()

install(TARGETS iso8583_core EXPORT iso8583-core-targets)
install(DIRECTORY include/ DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})
install(EXPORT iso8583-core-targets
    FILE iso8583-core-targets.cmake
    NAMESPACE iso8583::
    DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/iso8583-core)