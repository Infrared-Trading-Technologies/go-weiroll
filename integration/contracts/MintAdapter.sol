// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

interface INonfungiblePositionManager {
    struct MintParams {
        address token0;
        address token1;
        uint24 fee;
        int24 tickLower;
        int24 tickUpper;
        uint256 amount0Desired;
        uint256 amount1Desired;
        uint256 amount0Min;
        uint256 amount1Min;
        address recipient;
        uint256 deadline;
    }

    function mint(MintParams calldata params)
        external
        payable
        returns (
            uint256 tokenId,
            uint128 liquidity,
            uint256 amount0,
            uint256 amount1
        );
}

/// @title MintAdapter
/// @notice Library wrapper around UniswapV3 NonfungiblePositionManager.mint
/// with FLAT parameters so a weiroll planner can bind each argument to a
/// separate state slot (and thus to a *ReturnValue from an upstream
/// command). Intended to be registered as a weiroll Library (DELEGATECALL),
/// so msg.sender / approvals / token balances are evaluated in the VM's
/// context.
///
/// Convention: this contract is part of the "inline-ETH + delegatecall
/// adapter" pattern. When VM.execute() is called with msg.value > 0
/// (e.g. so a downstream WETH.deposit().WithValue(...) can wrap it), that
/// CALLVALUE is preserved through every DELEGATECALL inside _execute. A
/// nonpayable function's dispatcher reverts on CALLVALUE != 0 with empty
/// returndata, surfacing as ExecutionFailed(_, _, "Unknown") at the
/// failing command index. mintFlat is therefore marked `payable` to
/// suppress the dispatcher check. The function body never touches
/// msg.value — NPM.mint is invoked with value: 0, since NPM pulls tokens
/// via ERC20.transferFrom, not via msg.value.
///
/// To prevent ETH from being locked if someone CALLs mintFlat directly
/// (rather than DELEGATECALLing through a VM), the function asserts
/// `address(this) != _SELF`. Under DELEGATECALL, address(this) is the
/// VM, so the guard passes; under direct CALL it is _SELF and the call
/// reverts before any value is accepted. Same trick the Solidity
/// compiler uses internally to enforce library-only invocation.
contract MintAdapter {
    INonfungiblePositionManager public constant NPM =
        INonfungiblePositionManager(0xC36442b4a4522E871399CD717aBDD847Ab11FE88);

    /// @dev Set at deployment to this contract's own address; checked at
    /// runtime to reject direct CALLs (see contract-level docs).
    address private immutable _SELF;

    constructor() {
        _SELF = address(this);
    }

    /// @notice Flat-arg variant of NPM.mint. The 4-tuple return is the
    /// same as NPM.mint, so callers should use weiroll's RawReturn +
    /// TupleHelper.extract pattern to read individual fields.
    /// @dev `payable` is required for invocation via DELEGATECALL when
    /// the outer execute() carries msg.value; see contract-level docs.
    function mintFlat(
        address token0,
        address token1,
        uint24 fee,
        int24 tickLower,
        int24 tickUpper,
        uint256 amount0Desired,
        uint256 amount1Desired,
        uint256 amount0Min,
        uint256 amount1Min,
        address recipient,
        uint256 deadline
    )
        external
        payable
        returns (
            uint256 tokenId,
            uint128 liquidity,
            uint256 amount0,
            uint256 amount1
        )
    {
        require(address(this) != _SELF, "MintAdapter: delegatecall only");
        return
            NPM.mint(
                INonfungiblePositionManager.MintParams({
                    token0: token0,
                    token1: token1,
                    fee: fee,
                    tickLower: tickLower,
                    tickUpper: tickUpper,
                    amount0Desired: amount0Desired,
                    amount1Desired: amount1Desired,
                    amount0Min: amount0Min,
                    amount1Min: amount1Min,
                    recipient: recipient,
                    deadline: deadline
                })
            );
    }
}
