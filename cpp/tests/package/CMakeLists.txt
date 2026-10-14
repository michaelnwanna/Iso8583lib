cmake_minimum_required(VERSION 3.20)
project(iso8583_package_consumer LANGUAGES CXX)

find_package(iso8583_core 0.1 REQUIRED CONFIG)
add_executable(iso8583_package_consumer main.cpp)
target_link_libraries(iso8583_package_consumer PRIVATE iso8583::core)