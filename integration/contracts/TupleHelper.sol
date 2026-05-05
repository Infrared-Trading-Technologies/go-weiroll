// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/// @title TupleHelper
/// @notice Single-purpose helpers for extracting individual ABI words
/// from a raw bytes blob produced by a weiroll command with the tuple
/// return flag set.
///
/// Pair this with go-weiroll's `Call.RawReturn()` and
/// `ReturnValue.As(...)` to read individual fields from a tuple-
/// returning function.
contract TupleHelper {
    /// @notice Return the (32-byte) ABI word at `index` in `data`.
    /// @dev Interprets `data` as a tightly-packed sequence of 32-byte
    /// words, which is how weiroll stores the returndata of a function
    /// called with FLAG_TUPLE_RETURN: each ABI return slot occupies
    /// exactly one 32-byte word.
    function extract(bytes calldata data, uint256 index)
        external
        pure
        returns (bytes32 word)
    {
        uint256 offset = index * 32;
        require(data.length >= offset + 32, "TupleHelper: index out of range");
        assembly {
            word := calldataload(add(data.offset, offset))
        }
    }
}
