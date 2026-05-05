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
contract MintAdapter {
    INonfungiblePositionManager public constant NPM =
        INonfungiblePositionManager(0xC36442b4a4522E871399CD717aBDD847Ab11FE88);

    /// @notice Flat-arg variant of NPM.mint. The 4-tuple return is the
    /// same as NPM.mint, so callers should use weiroll's RawReturn +
    /// TupleHelper.extract pattern to read individual fields.
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
        returns (
            uint256 tokenId,
            uint128 liquidity,
            uint256 amount0,
            uint256 amount1
        )
    {
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
