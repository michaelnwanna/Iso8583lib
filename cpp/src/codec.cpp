#include "iso8583/codec.hpp"

#include <stdexcept>

namespace iso8583 {
namespace {

void check_number(std::size_t number) {
    if (number < 2 || number > max_field) {
        throw std::out_of_range("iso8583: field number is out of range");
    }
}

void append_length(std::vector<std::uint8_t>& output, std::size_t length, std::size_t digits) {
    const auto start = output.size();
    output.resize(start + digits);
    for (auto index = start + digits; index-- > start;) {
        output[index] = static_cast<std::uint8_t>('0' + length % 10);
        length /= 10;
    }
}

std::size_t parse_length(const std::uint8_t* data, std::size_t size, std::size_t offset, std::size_t digits) {
    if (offset > size || digits > size - offset) {
        throw std::invalid_argument("iso8583: truncated field length");
    }
    std::size_t length = 0;
    for (std::size_t index = 0; index < digits; ++index) {
        const auto digit = data[offset + index];
        if (digit < '0' || digit > '9') {
            throw std::invalid_argument("iso8583: non-decimal field length");
        }
        length = length * 10 + digit - '0';
    }
    return length;
}

}  // namespace

spec::spec(const std::unordered_map<std::size_t, field_spec>& fields) {
    for (const auto& entry : fields) {
        const auto number = entry.first;
        const auto value = entry.second;
        check_number(number);
        if (value.length == 0 ||
            (value.type == length_type::llvar && value.length > 99) ||
            (value.type == length_type::lllvar && value.length > 999)) {
            throw std::invalid_argument("iso8583: invalid field specification");
        }
        fields_[number] = value;
    }
}

const field_spec& spec::operator[](std::size_t number) const noexcept { return fields_[number]; }

message::message(std::string_view mti) {
    if (mti.size() != mti_.size()) {
        throw std::invalid_argument("iso8583: MTI must contain exactly 4 bytes");
    }
    std::copy(mti.begin(), mti.end(), mti_.begin());
}

std::string_view message::mti() const noexcept { return {mti_.data(), mti_.size()}; }

std::string_view message::field(std::size_t number) const {
    check_number(number);
    return fields_[number];
}

void message::set_field(std::size_t number, std::string value) {
    check_number(number);
    fields_[number] = std::move(value);
}

void message::append_to(std::vector<std::uint8_t>& destination, const spec& message_spec) const {
    std::size_t bitmap_bytes = 8;
    for (std::size_t number = 65; number <= max_field; ++number) {
        if (!fields_[number].empty()) {
            bitmap_bytes = 16;
            break;
        }
    }

    const auto start = destination.size();
    destination.insert(destination.end(), mti_.begin(), mti_.end());
    destination.resize(destination.size() + bitmap_bytes, 0);
    auto* bitmap = destination.data() + start + 4;
    if (bitmap_bytes == 16) {
        bitmap[0] |= 0x80;
    }

    try {
        for (std::size_t number = 2; number <= max_field; ++number) {
            const auto& value = fields_[number];
            if (value.empty()) {
                continue;
            }
            const auto field = message_spec[number];
            if (field.length == 0) {
                throw std::invalid_argument("iso8583: field is not defined");
            }
            if ((field.type == length_type::fixed && value.size() != field.length) ||
                (field.type != length_type::fixed && value.size() > field.length)) {
                throw std::invalid_argument("iso8583: field has invalid length");
            }
            const auto bit = number - 1;
            bitmap[bit / 8] |= static_cast<std::uint8_t>(1U << (7U - bit % 8));
            if (field.type == length_type::llvar) {
                append_length(destination, value.size(), 2);
            } else if (field.type == length_type::lllvar) {
                append_length(destination, value.size(), 3);
            }
            destination.insert(destination.end(), value.begin(), value.end());
            bitmap = destination.data() + start + 4;
        }
    } catch (...) {
        destination.resize(start);
        throw;
    }
}

message parse(const std::uint8_t* data, std::size_t size, const spec& message_spec) {
    if (size < 12) {
        throw std::invalid_argument("iso8583: message is shorter than MTI and primary bitmap");
    }
    message output({reinterpret_cast<const char*>(data), 4});
    std::size_t bitmap_bytes = 8;
    std::size_t offset = 12;
    if ((data[4] & 0x80) != 0) {
        if (size < 20) {
            throw std::invalid_argument("iso8583: secondary bitmap is truncated");
        }
        bitmap_bytes = 16;
        offset = 20;
    }

    for (std::size_t number = 2; number <= bitmap_bytes * 8; ++number) {
        const auto bit = number - 1;
        if ((data[4 + bit / 8] & (1U << (7U - bit % 8))) == 0) {
            continue;
        }
        const auto field = message_spec[number];
        if (field.length == 0) {
            throw std::invalid_argument("iso8583: field is not defined");
        }
        auto length = field.length;
        const auto prefix = field.type == length_type::llvar ? 2U : field.type == length_type::lllvar ? 3U : 0U;
        if (prefix != 0) {
            length = parse_length(data, size, offset, prefix);
            offset += prefix;
            if (length > field.length) {
                throw std::invalid_argument("iso8583: field exceeds maximum length");
            }
        }
        if (offset > size || length > size - offset) {
            throw std::invalid_argument("iso8583: field is truncated");
        }
        output.fields_[number].assign(reinterpret_cast<const char*>(data + offset), length);
        offset += length;
    }
    if (offset != size) {
        throw std::invalid_argument("iso8583: trailing bytes");
    }
    return output;
}

}  // namespace iso8583