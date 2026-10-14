#include "iso8583/codec.hpp"

#include <chrono>
#include <cstddef>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>

int main(int argc, char** argv) {
    const auto iterations = argc > 1 ? std::strtoull(argv[1], nullptr, 10) : 1000000ULL;
    if (iterations == 0) return 1;

    const iso8583::spec spec({
        {2, {iso8583::length_type::llvar, 19}},
        {3, {iso8583::length_type::fixed, 6}},
        {4, {iso8583::length_type::fixed, 12}},
        {11, {iso8583::length_type::fixed, 6}},
        {41, {iso8583::length_type::fixed, 8}},
    });
    iso8583::message message("0100");
    message.set_field(2, "4111111111111111");
    message.set_field(3, "000000");
    message.set_field(4, "000000012500");
    message.set_field(11, "123456");
    message.set_field(41, "TERM0001");
    std::vector<std::uint8_t> buffer;
    buffer.reserve(128);

    for (std::size_t index = 0; index < 10000; ++index) {
        buffer.clear();
        message.append_to(buffer, spec);
        static_cast<void>(iso8583::parse(buffer, spec));
    }

    const auto start = std::chrono::steady_clock::now();
    for (std::size_t index = 0; index < iterations; ++index) {
        buffer.clear();
        message.append_to(buffer, spec);
        static_cast<void>(iso8583::parse(buffer, spec));
    }
    const auto elapsed = std::chrono::steady_clock::now() - start;
    const auto nanoseconds = std::chrono::duration<double, std::nano>(elapsed).count() / iterations;
    std::cout << "roundtrip: " << nanoseconds << " ns/op, " << iterations << " iterations\n";
}