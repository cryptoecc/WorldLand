// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

interface IPriceOracle {
    function priceAt(bytes32 asset, uint64 hourId)
        external
        view
        returns (uint128 price, uint64 timestamp, bool posted);
}

/// @title WorldBet
/// @notice Hourly multi-asset pari-mutuel price-prediction market.
///         For each registered asset, every UTC hour opens a new round.
///         Bettors stake native WL on UP or DOWN. Winners split the loser
///         pool pro-rata. Fees: 1% prize pool, 0.3% sticky referrer rebate,
///         1.7% accumulated for permissionless burn to 0x...dEaD.
contract WorldBet {
    address public constant DEAD = 0x000000000000000000000000000000000000dEaD;

    uint64 public constant ROUND_DURATION = 3600; // 1 hour
    uint64 public constant ORACLE_GRACE = 1800;   // 30 min refund window

    // Fee split in basis points (denominator 10000).
    uint16 public constant FEE_BPS_TOTAL    = 300; // 3.0%
    uint16 public constant FEE_BPS_PRIZE    = 100; // 1.0%
    uint16 public constant FEE_BPS_REFERRAL = 30;  // 0.3%
    // Burn share = TOTAL - PRIZE - REFERRAL = 170 bps (1.7%).

    enum Direction { Up, Down }

    // status: 0 open, 1 locked, 2 settled-UP, 3 settled-DOWN, 4 refund.
    struct Round {
        uint128 upPool;
        uint128 downPool;
        uint64 lockTime;
        uint64 closeTime;
        uint128 lockPrice;
        uint128 closePrice;
        uint8 status;
    }

    struct Bet {
        uint128 upAmount;
        uint128 downAmount;
        bool claimed;
    }

    IPriceOracle public immutable oracle;
    address public owner;

    bytes32[] public assets;
    mapping(bytes32 => bool) public assetRegistered;
    mapping(bytes32 => uint128) public maxBetPerRound; // 0 = no cap

    mapping(bytes32 => mapping(uint64 => Round)) public rounds;
    mapping(bytes32 => mapping(uint64 => mapping(address => Bet))) public bets;

    mapping(address => address) public referrer;        // sticky, set on first bet
    mapping(address => uint256) public referralBalance; // claimable

    uint256 public prizePool;     // 1% accumulator (admin distribution)
    uint256 public pendingBurn;   // 1.7% accumulator (permissionless burn)
    uint256 public totalBurned;   // lifetime sent to 0x...dEaD

    event AssetRegistered(bytes32 indexed asset);
    event MaxBetSet(bytes32 indexed asset, uint128 cap);
    event BetPlaced(bytes32 indexed asset, uint64 indexed roundId, address indexed user, Direction dir, uint256 amount);
    event RoundLocked(bytes32 indexed asset, uint64 indexed roundId, uint128 lockPrice);
    event RoundSettled(bytes32 indexed asset, uint64 indexed roundId, uint128 closePrice, uint8 status);
    event RoundRefunded(bytes32 indexed asset, uint64 indexed roundId, string reason);
    event Claimed(bytes32 indexed asset, uint64 indexed roundId, address indexed user, uint256 amount);
    event Burned(uint256 amount, uint256 totalBurned);
    event PrizeDistributed(address indexed to, uint256 amount);
    event ReferralPaid(address indexed referrer, address indexed bettor, uint256 amount);
    event ReferralClaimed(address indexed referrer, uint256 amount);
    event OwnerSet(address indexed owner);

    modifier onlyOwner() {
        require(msg.sender == owner, "WB: not owner");
        _;
    }

    constructor(address _oracle, address _owner) {
        require(_oracle != address(0), "WB: oracle=0");
        require(_owner != address(0), "WB: owner=0");
        oracle = IPriceOracle(_oracle);
        owner = _owner;
    }

    // -------- admin --------

    function setOwner(address _owner) external onlyOwner {
        require(_owner != address(0), "WB: owner=0");
        owner = _owner;
        emit OwnerSet(_owner);
    }

    function registerAsset(bytes32 key) external onlyOwner {
        require(!assetRegistered[key], "WB: dup");
        assetRegistered[key] = true;
        assets.push(key);
        emit AssetRegistered(key);
    }

    function setMaxBetPerRound(bytes32 asset, uint128 cap) external onlyOwner {
        require(assetRegistered[asset], "WB: bad asset");
        maxBetPerRound[asset] = cap;
        emit MaxBetSet(asset, cap);
    }

    // -------- views --------

    function currentRoundId() public view returns (uint64) {
        return uint64(block.timestamp / ROUND_DURATION);
    }

    function assetCount() external view returns (uint256) {
        return assets.length;
    }

    /// @notice Convenience accessor returning round + caller bet in one call.
    function roundView(bytes32 asset, uint64 id, address user)
        external
        view
        returns (Round memory r, Bet memory b)
    {
        r = rounds[asset][id];
        b = bets[asset][id][user];
    }

    // -------- core --------

    /// @notice Place a bet on the currently-open round for `asset`.
    /// @param asset registered asset key (e.g. keccak256("WL/USD"))
    /// @param dir Up or Down
    /// @param ref optional referrer (sticky once set; zero/self ignored)
    function bet(bytes32 asset, Direction dir, address ref) external payable {
        require(assetRegistered[asset], "WB: bad asset");
        require(msg.value > 0, "WB: zero");

        uint64 id = currentRoundId();
        Round storage r = _ensureRound(asset, id);
        require(block.timestamp < r.lockTime, "WB: locked");

        // Sticky referrer: first non-self non-zero ref wins forever.
        if (ref != address(0) && ref != msg.sender && referrer[msg.sender] == address(0)) {
            referrer[msg.sender] = ref;
        }

        uint256 amt = msg.value;
        uint256 prizePart = (amt * FEE_BPS_PRIZE) / 10000;
        uint256 refPart   = (amt * FEE_BPS_REFERRAL) / 10000;
        uint256 burnPart  = (amt * (FEE_BPS_TOTAL - FEE_BPS_PRIZE - FEE_BPS_REFERRAL)) / 10000;
        uint256 net       = amt - prizePart - refPart - burnPart;

        prizePool += prizePart;

        address actualRef = referrer[msg.sender];
        if (actualRef != address(0)) {
            referralBalance[actualRef] += refPart;
            emit ReferralPaid(actualRef, msg.sender, refPart);
        } else {
            burnPart += refPart; // no referrer => burn the rebate share
        }
        pendingBurn += burnPart;

        Bet storage b = bets[asset][id][msg.sender];
        uint128 cap = maxBetPerRound[asset];
        if (cap > 0) {
            require(uint256(b.upAmount) + b.downAmount + net <= cap, "WB: cap");
        }

        if (dir == Direction.Up) {
            b.upAmount += uint128(net);
            r.upPool   += uint128(net);
        } else {
            b.downAmount += uint128(net);
            r.downPool   += uint128(net);
        }

        emit BetPlaced(asset, id, msg.sender, dir, net);
    }

    /// @notice Lock a round once its lockTime has passed: pulls oracle
    ///         price for hourId = lockTime/ROUND_DURATION. If oracle has
    ///         not posted within ORACLE_GRACE the round refunds.
    function lockRound(bytes32 asset, uint64 id) external {
        Round storage r = rounds[asset][id];
        require(r.lockTime != 0, "WB: no round");
        require(r.status == 0, "WB: not open");
        require(block.timestamp >= r.lockTime, "WB: too early");

        (uint128 price, , bool posted) = oracle.priceAt(asset, uint64(r.lockTime / ROUND_DURATION));
        if (!posted) {
            if (block.timestamp >= r.lockTime + ORACLE_GRACE) {
                r.status = 4;
                emit RoundRefunded(asset, id, "lock-oracle-missed");
                return;
            }
            revert("WB: oracle pending");
        }
        r.lockPrice = price;
        r.status = 1;
        emit RoundLocked(asset, id, price);
    }

    /// @notice Settle a round once its closeTime has passed: pulls close
    ///         oracle price and computes outcome. One-sided pool, tie, or
    ///         missing oracle within grace -> refund.
    function settleRound(bytes32 asset, uint64 id) external {
        Round storage r = rounds[asset][id];
        require(r.lockTime != 0, "WB: no round");
        require(r.status == 0 || r.status == 1, "WB: settled");
        require(block.timestamp >= r.closeTime, "WB: too early");

        if (r.status == 0) {
            // Round was never locked. After grace, refund.
            if (block.timestamp >= r.lockTime + ORACLE_GRACE) {
                r.status = 4;
                emit RoundRefunded(asset, id, "lock-skipped");
                return;
            }
            revert("WB: lock first");
        }

        if (r.upPool == 0 || r.downPool == 0) {
            r.status = 4;
            emit RoundRefunded(asset, id, "one-sided");
            return;
        }

        (uint128 price, , bool posted) = oracle.priceAt(asset, uint64(r.closeTime / ROUND_DURATION));
        if (!posted) {
            if (block.timestamp >= r.closeTime + ORACLE_GRACE) {
                r.status = 4;
                emit RoundRefunded(asset, id, "close-oracle-missed");
                return;
            }
            revert("WB: oracle pending");
        }

        r.closePrice = price;
        if (price == r.lockPrice) {
            r.status = 4;
            emit RoundRefunded(asset, id, "tie");
        } else if (price > r.lockPrice) {
            r.status = 2;
            emit RoundSettled(asset, id, price, 2);
        } else {
            r.status = 3;
            emit RoundSettled(asset, id, price, 3);
        }
    }

    /// @notice Claim winnings (or refund) for a finalized round.
    function claim(bytes32 asset, uint64 id) external {
        Round storage r = rounds[asset][id];
        Bet storage b = bets[asset][id][msg.sender];
        require(r.status >= 2, "WB: not final");
        require(!b.claimed, "WB: claimed");
        b.claimed = true;

        uint256 payout;
        if (r.status == 4) {
            // Refund: stake back, both directions.
            payout = uint256(b.upAmount) + uint256(b.downAmount);
        } else if (r.status == 2 && b.upAmount > 0) {
            // UP wins: stake + share of DOWN pool pro-rata.
            payout = uint256(b.upAmount) + (uint256(b.upAmount) * uint256(r.downPool)) / uint256(r.upPool);
        } else if (r.status == 3 && b.downAmount > 0) {
            payout = uint256(b.downAmount) + (uint256(b.downAmount) * uint256(r.upPool)) / uint256(r.downPool);
        }

        require(payout > 0, "WB: nothing");
        (bool ok, ) = payable(msg.sender).call{value: payout}("");
        require(ok, "WB: send fail");
        emit Claimed(asset, id, msg.sender, payout);
    }

    function claimReferral() external {
        uint256 amt = referralBalance[msg.sender];
        require(amt > 0, "WB: zero");
        referralBalance[msg.sender] = 0;
        (bool ok, ) = payable(msg.sender).call{value: amt}("");
        require(ok, "WB: send fail");
        emit ReferralClaimed(msg.sender, amt);
    }

    /// @notice Anyone can trigger the deflationary burn of accumulated fees.
    function burn() external {
        uint256 amt = pendingBurn;
        require(amt > 0, "WB: zero");
        pendingBurn = 0;
        totalBurned += amt;
        (bool ok, ) = payable(DEAD).call{value: amt}("");
        require(ok, "WB: send fail");
        emit Burned(amt, totalBurned);
    }

    /// @notice Owner distributes the weekly leaderboard prize pool. The
    ///         leaderboard itself is computed off-chain from BetPlaced /
    ///         Claimed events; only the payout is on-chain.
    function distributePrize(address[] calldata recipients, uint256[] calldata amounts) external onlyOwner {
        require(recipients.length == amounts.length, "WB: len");
        uint256 sum;
        for (uint256 i = 0; i < amounts.length; i++) sum += amounts[i];
        require(sum <= prizePool, "WB: > pool");
        prizePool -= sum;
        for (uint256 i = 0; i < recipients.length; i++) {
            (bool ok, ) = payable(recipients[i]).call{value: amounts[i]}("");
            require(ok, "WB: send fail");
            emit PrizeDistributed(recipients[i], amounts[i]);
        }
    }

    // -------- internal --------

    function _ensureRound(bytes32 asset, uint64 id) internal returns (Round storage r) {
        r = rounds[asset][id];
        if (r.lockTime == 0) {
            r.lockTime = uint64((id + 1) * ROUND_DURATION);
            r.closeTime = uint64((id + 2) * ROUND_DURATION);
        }
    }

    receive() external payable {
        // Direct sends top up the burn accumulator. Useful for community
        // donations / treasury seeding without affecting accounting.
        pendingBurn += msg.value;
    }
}
