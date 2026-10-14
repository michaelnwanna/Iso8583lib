#pragma once

#include "iso8583/codec.hpp"

#include <array>
#include <cstddef>
#include <cstdint>
#include <istream>
#include <string>
#include <string_view>
#include <unordered_map>
#include <vector>

namespace iso8583 {

enum class data_encoding : std::uint8_t { ascii, binary };
enum class content_type : std::uint8_t { numeric, text, bytes };
enum class length_encoding : std::uint8_t { none, binary_big_endian, ascii };

struct field_definition {
    field_spec field{};
    std::string name;
    data_encoding encoding{};
    content_type content{};
    bool sensitive{};
};

struct message_rule {
    std::vector<std::size_t> required;
};

class transport_spec {
public:
    transport_spec() = default;
    transport_spec(std::size_t header_bytes, length_encoding encoding, std::size_t max_message_bytes);

    std::size_t header_bytes() const noexcept { return header_bytes_; }
    length_encoding encoding() const noexcept { return encoding_; }
    std::size_t max_message_bytes() const noexcept { return max_message_bytes_; }

    void append_frame(std::vector<std::uint8_t>& destination, const std::uint8_t* payload, std::size_t size) const;
    void append_frame(std::vector<std::uint8_t>& destination, const std::vector<std::uint8_t>& payload) const {
        append_frame(destination, payload.data(), payload.size());
    }
    std::string_view unframe(const std::uint8_t* frame, std::size_t size) const;
    std::string_view unframe(const std::vector<std::uint8_t>& frame) const {
        return unframe(frame.data(), frame.size());
    }

private:
    std::size_t header_bytes_{};
    length_encoding encoding_{length_encoding::none};
    std::size_t max_message_bytes_{};
};

class profile {
public:
    static profile load_json(std::istream& input);
    static profile parse_json(std::string_view json);

    const std::string& name() const noexcept { return name_; }
    const spec& message_spec() const noexcept { return spec_; }
    const transport_spec& transport() const noexcept { return transport_; }
    const field_definition& field(std::size_t number) const;
    void validate(const message& value) const;

private:
    profile(std::string name, spec message_spec, std::array<field_definition, max_field + 1> fields,
            std::unordered_map<std::string, message_rule> messages, transport_spec transport);

    std::string name_;
    spec spec_;
    std::array<field_definition, max_field + 1> fields_{};
    std::unordered_map<std::string, message_rule> messages_;
    transport_spec transport_;
};

}  // namespace iso8583