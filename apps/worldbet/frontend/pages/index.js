import { useEffect, useMemo, useState } from "react";
import { useAccount, useConnect, useDisconnect, useReadContract, useWriteContract, useChainId } from "wagmi";
import { keccak256, parseEther, formatEther, toBytes, zeroAddress } from "viem";
import { WORLDBET_ABI } from "../lib/abi";
import { ASSETS, WORLDBET_ADDRESS, seoul } from "../lib/chain";

const assetKey = (label) => keccak256(toBytes(label));

function useReferrer() {
  const [ref, setRef] = useState(zeroAddress);
  useEffect(() => {
    if (typeof window === "undefined") return;
    const params = new URLSearchParams(window.location.search);
    const r = params.get("ref");
    if (r && /^0x[0-9a-fA-F]{40}$/.test(r)) {
      setRef(r);
      try { localStorage.setItem("worldbet:ref", r); } catch {}
    } else {
      try {
        const cached = localStorage.getItem("worldbet:ref");
        if (cached && /^0x[0-9a-fA-F]{40}$/.test(cached)) setRef(cached);
      } catch {}
    }
  }, []);
  return ref;
}

function fmtWL(v) {
  if (v === undefined || v === null) return "—";
  const n = Number(formatEther(v));
  if (n === 0) return "0";
  if (n < 0.001) return n.toExponential(2);
  return n.toLocaleString(undefined, { maximumFractionDigits: 4 });
}

function AssetCard({ label, account, ref }) {
  const key = useMemo(() => assetKey(label), [label]);
  const { data: roundId } = useReadContract({
    address: WORLDBET_ADDRESS, abi: WORLDBET_ABI, functionName: "currentRoundId",
    query: { refetchInterval: 5000 },
  });
  const { data: rv } = useReadContract({
    address: WORLDBET_ADDRESS, abi: WORLDBET_ABI, functionName: "roundView",
    args: roundId !== undefined ? [key, roundId, account || zeroAddress] : undefined,
    query: { enabled: roundId !== undefined, refetchInterval: 5000 },
  });
  const { writeContract, isPending } = useWriteContract();

  const [amount, setAmount] = useState("0.1");
  const placeBet = (dir) => {
    if (!WORLDBET_ADDRESS || !account) return;
    writeContract({
      address: WORLDBET_ADDRESS, abi: WORLDBET_ABI,
      functionName: "bet",
      args: [key, dir, ref || zeroAddress],
      value: parseEther(amount || "0"),
    });
  };

  const round = rv?.[0];
  const bet = rv?.[1];
  const lockTs = round?.lockTime ? Number(round.lockTime) : 0;
  const now = Math.floor(Date.now() / 1000);
  const secLeft = Math.max(0, lockTs - now);
  const upPool = round?.upPool ?? 0n;
  const downPool = round?.downPool ?? 0n;
  const total = upPool + downPool;
  const upPct = total > 0n ? Number((upPool * 10000n) / total) / 100 : 50;

  return (
    <div className="card">
      <div className="row between">
        <h2>{label}</h2>
        <div className="muted">round #{roundId?.toString?.() ?? "—"}</div>
      </div>
      <div className="row between mt8">
        <div>UP pool: <b>{fmtWL(upPool)}</b> WL</div>
        <div>DOWN pool: <b>{fmtWL(downPool)}</b> WL</div>
      </div>
      <div className="bar mt8"><div style={{ width: `${upPct}%` }} /></div>
      <div className="row between mt8 muted">
        <span>{upPct.toFixed(1)}% UP</span>
        <span>locks in {Math.floor(secLeft / 60)}:{String(secLeft % 60).padStart(2, "0")}</span>
      </div>
      <div className="row mt12">
        <input value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="WL" />
        <button onClick={() => placeBet(0)} disabled={!account || isPending}>UP</button>
        <button onClick={() => placeBet(1)} disabled={!account || isPending} className="down">DOWN</button>
      </div>
      {bet && (Number(bet.upAmount) > 0 || Number(bet.downAmount) > 0) && (
        <div className="muted mt8">
          your bet: UP {fmtWL(bet.upAmount)} / DOWN {fmtWL(bet.downAmount)}
          {bet.claimed ? " (claimed)" : ""}
        </div>
      )}
    </div>
  );
}

function BurnCounter() {
  const { data: total } = useReadContract({
    address: WORLDBET_ADDRESS, abi: WORLDBET_ABI, functionName: "totalBurned",
    query: { refetchInterval: 10000 },
  });
  const { data: pending } = useReadContract({
    address: WORLDBET_ADDRESS, abi: WORLDBET_ABI, functionName: "pendingBurn",
    query: { refetchInterval: 10000 },
  });
  const { writeContract, isPending } = useWriteContract();
  return (
    <div className="card">
      <h2>Deflation</h2>
      <div className="row between mt8">
        <div>burned to 0x...dEaD: <b>{fmtWL(total)}</b> WL</div>
        <div>pending: <b>{fmtWL(pending)}</b> WL</div>
      </div>
      <button
        className="mt12"
        disabled={!pending || pending === 0n || isPending}
        onClick={() => writeContract({ address: WORLDBET_ADDRESS, abi: WORLDBET_ABI, functionName: "burn", args: [] })}
      >
        Trigger burn
      </button>
    </div>
  );
}

function ReferralPanel({ account }) {
  const { data: claimable } = useReadContract({
    address: WORLDBET_ADDRESS, abi: WORLDBET_ABI, functionName: "referralBalance",
    args: account ? [account] : undefined,
    query: { enabled: !!account, refetchInterval: 10000 },
  });
  const { writeContract, isPending } = useWriteContract();
  const link = typeof window !== "undefined" && account
    ? `${window.location.origin}/?ref=${account}`
    : "";
  return (
    <div className="card">
      <h2>Referrals</h2>
      <div className="muted">share your link, earn 0.3% of every bet your invitees place. sticky for life.</div>
      {link && (
        <input
          readOnly
          className="mt8 mono"
          value={link}
          onClick={(e) => e.target.select()}
        />
      )}
      <div className="row between mt12">
        <div>claimable: <b>{fmtWL(claimable)}</b> WL</div>
        <button
          disabled={!claimable || claimable === 0n || isPending}
          onClick={() => writeContract({ address: WORLDBET_ADDRESS, abi: WORLDBET_ABI, functionName: "claimReferral", args: [] })}
        >
          Claim
        </button>
      </div>
    </div>
  );
}

export default function Home() {
  const { address, isConnected } = useAccount();
  const { connect, connectors } = useConnect();
  const { disconnect } = useDisconnect();
  const chainId = useChainId();
  const ref = useReferrer();
  const wrongChain = isConnected && chainId !== seoul.id;

  return (
    <main>
      <header className="row between">
        <h1>WorldBet</h1>
        <div>
          {isConnected ? (
            <button onClick={() => disconnect()}>{address.slice(0, 6)}…{address.slice(-4)}</button>
          ) : (
            connectors.map((c) => (
              <button key={c.uid} onClick={() => connect({ connector: c })}>
                Connect {c.name}
              </button>
            ))
          )}
        </div>
      </header>

      {!WORLDBET_ADDRESS && (
        <div className="warn">Set NEXT_PUBLIC_WORLDBET_ADDRESS in .env.local before betting.</div>
      )}
      {wrongChain && <div className="warn">Switch to WorldLand Seoul (chainId 103).</div>}

      <section className="grid mt16">
        {ASSETS.map((a) => (
          <AssetCard key={a.key} label={a.key} account={address} ref={ref} />
        ))}
      </section>

      <section className="grid mt16">
        <BurnCounter />
        <ReferralPanel account={address} />
      </section>

      <footer className="muted mt24">
        Pari-mutuel, native WL, hourly rounds. 3% fee = 1% prize / 0.3% ref / 1.7% burn.
      </footer>

      <style jsx global>{`
        :root { color-scheme: dark; }
        body { margin: 0; font-family: ui-sans-serif, system-ui, sans-serif; background: #0a0a0a; color: #eaeaea; }
        main { max-width: 960px; margin: 0 auto; padding: 24px; }
        header h1 { margin: 0; font-size: 28px; letter-spacing: 0.5px; }
        h2 { margin: 0; font-size: 18px; }
        .row { display: flex; gap: 8px; align-items: center; }
        .between { justify-content: space-between; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 16px; }
        .card { background: #141414; border: 1px solid #222; border-radius: 12px; padding: 16px; }
        .mt8 { margin-top: 8px; } .mt12 { margin-top: 12px; } .mt16 { margin-top: 16px; } .mt24 { margin-top: 24px; }
        .muted { color: #888; font-size: 13px; }
        .mono { font-family: ui-monospace, Menlo, monospace; font-size: 12px; }
        input { background: #0d0d0d; color: #eaeaea; border: 1px solid #2a2a2a; padding: 8px 10px; border-radius: 8px; flex: 1; }
        button { background: #2a7d2e; color: #fff; border: none; padding: 8px 14px; border-radius: 8px; cursor: pointer; font-weight: 600; }
        button.down { background: #b03030; }
        button:disabled { opacity: 0.5; cursor: not-allowed; }
        .bar { height: 6px; background: #b03030; border-radius: 3px; overflow: hidden; }
        .bar > div { height: 100%; background: #2a7d2e; }
        .warn { background: #2a1a00; border: 1px solid #553; color: #fc6; padding: 10px 12px; border-radius: 8px; margin-top: 12px; }
        footer { font-size: 12px; }
      `}</style>
    </main>
  );
}
