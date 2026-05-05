// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title WorldBet PriceOracle
/// @notice M-of-N EIP-712 signed price feed. Signers post the median CEX
///         price for an (asset, hourId) tuple; the contract verifies
///         signatures and stores the result for WorldBet rounds to read.
contract PriceOracle {
    string public constant NAME = "WorldBet PriceOracle";
    string public constant VERSION = "1";

    bytes32 public constant PRICE_TYPEHASH =
        keccak256("Price(bytes32 asset,uint64 hourId,uint128 price,uint64 timestamp)");

    bytes32 public immutable DOMAIN_SEPARATOR;

    address public owner;
    uint8 public threshold;
    address[] public signers;
    mapping(address => bool) public isSigner;

    struct Posted {
        uint128 price;
        uint64 timestamp;
        bool exists;
    }

    // asset => hourId => posted price
    mapping(bytes32 => mapping(uint64 => Posted)) private _prices;

    event PricePosted(bytes32 indexed asset, uint64 indexed hourId, uint128 price, uint64 timestamp);
    event SignerAdded(address indexed signer);
    event SignerRemoved(address indexed signer);
    event ThresholdSet(uint8 threshold);
    event OwnerSet(address indexed owner);

    modifier onlyOwner() {
        require(msg.sender == owner, "PO: not owner");
        _;
    }

    constructor(address _owner, address[] memory _signers, uint8 _threshold) {
        require(_owner != address(0), "PO: owner=0");
        require(_signers.length > 0, "PO: no signers");
        require(_threshold > 0 && _threshold <= _signers.length, "PO: bad threshold");

        owner = _owner;
        for (uint256 i = 0; i < _signers.length; i++) {
            address s = _signers[i];
            require(s != address(0), "PO: signer=0");
            require(!isSigner[s], "PO: dup");
            isSigner[s] = true;
            signers.push(s);
        }
        threshold = _threshold;

        DOMAIN_SEPARATOR = keccak256(abi.encode(
            keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
            keccak256(bytes(NAME)),
            keccak256(bytes(VERSION)),
            block.chainid,
            address(this)
        ));
    }

    function setOwner(address _owner) external onlyOwner {
        require(_owner != address(0), "PO: owner=0");
        owner = _owner;
        emit OwnerSet(_owner);
    }

    function addSigner(address s) external onlyOwner {
        require(s != address(0), "PO: signer=0");
        require(!isSigner[s], "PO: dup");
        isSigner[s] = true;
        signers.push(s);
        emit SignerAdded(s);
    }

    function removeSigner(address s) external onlyOwner {
        require(isSigner[s], "PO: !signer");
        isSigner[s] = false;
        uint256 n = signers.length;
        for (uint256 i = 0; i < n; i++) {
            if (signers[i] == s) {
                signers[i] = signers[n - 1];
                signers.pop();
                break;
            }
        }
        require(signers.length >= threshold, "PO: under threshold");
        emit SignerRemoved(s);
    }

    function setThreshold(uint8 t) external onlyOwner {
        require(t > 0 && t <= signers.length, "PO: bad threshold");
        threshold = t;
        emit ThresholdSet(t);
    }

    /// @notice Post a price for (asset, hourId). Caller can be anyone; auth
    ///         comes from the supplied EIP-712 signatures.
    function postPrice(
        bytes32 asset,
        uint64 hourId,
        uint128 price,
        uint64 timestamp,
        bytes[] calldata signatures
    ) external {
        require(!_prices[asset][hourId].exists, "PO: posted");
        require(price > 0, "PO: zero price");
        require(signatures.length >= threshold, "PO: < threshold");

        bytes32 structHash = keccak256(abi.encode(PRICE_TYPEHASH, asset, hourId, price, timestamp));
        bytes32 digest = keccak256(abi.encodePacked("\x19\x01", DOMAIN_SEPARATOR, structHash));

        address[] memory seen = new address[](signatures.length);
        uint256 valid;
        for (uint256 i = 0; i < signatures.length; i++) {
            address signer = _recover(digest, signatures[i]);
            if (signer == address(0) || !isSigner[signer]) continue;
            bool dup;
            for (uint256 j = 0; j < valid; j++) {
                if (seen[j] == signer) { dup = true; break; }
            }
            if (dup) continue;
            seen[valid++] = signer;
            if (valid >= threshold) break;
        }
        require(valid >= threshold, "PO: not enough valid sigs");

        _prices[asset][hourId] = Posted({price: price, timestamp: timestamp, exists: true});
        emit PricePosted(asset, hourId, price, timestamp);
    }

    function priceAt(bytes32 asset, uint64 hourId)
        external
        view
        returns (uint128 price, uint64 timestamp, bool posted)
    {
        Posted storage p = _prices[asset][hourId];
        return (p.price, p.timestamp, p.exists);
    }

    function signerCount() external view returns (uint256) {
        return signers.length;
    }

    function _recover(bytes32 digest, bytes calldata sig) internal pure returns (address) {
        if (sig.length != 65) return address(0);
        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly {
            r := calldataload(sig.offset)
            s := calldataload(add(sig.offset, 32))
            v := byte(0, calldataload(add(sig.offset, 64)))
        }
        if (v < 27) v += 27;
        if (v != 27 && v != 28) return address(0);
        // Reject high-s to prevent malleability.
        if (uint256(s) > 0x7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF5D576E7357A4501DDFE92F46681B20A0) {
            return address(0);
        }
        return ecrecover(digest, v, r, s);
    }
}
