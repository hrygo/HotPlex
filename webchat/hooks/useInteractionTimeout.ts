import { useEffect, useState } from "react";
import type { InteractionStatus } from "@/lib/adapters/hotplex-runtime-adapter";

export function useInteractionTimeout(
  initialStatus: InteractionStatus,
  expiresAt?: number
) {
  const [timeLeft, setTimeLeft] = useState<number | null>(null);

  useEffect(() => {
    if (initialStatus !== "pending" || !expiresAt) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setTimeLeft(null);
      return;
    }
    const updateTime = () => {
      const diff = Math.max(0, Math.floor((expiresAt - Date.now()) / 1000));
      setTimeLeft(diff);
    };
    updateTime();
    const interval = setInterval(updateTime, 1000);
    return () => clearInterval(interval);
  }, [initialStatus, expiresAt]);

  const activeStatus: InteractionStatus =
    initialStatus === "pending" && timeLeft !== null && timeLeft <= 0 ? "expired" : initialStatus;

  return { timeLeft, activeStatus };
}
