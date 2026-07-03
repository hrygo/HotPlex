"use client";

import { useCallback, useEffect, useRef } from "react";

/**
 * Timeout helper: schedule a single delayed callback that is cancelable and
 * automatically cleared on unmount. Replaces the manual
 * `useRef<ReturnType<typeof setTimeout>>` + `clearTimeout` + unmount-cleanup
 * `useEffect` idiom duplicated across the codebase (hover-to-close dropdowns,
 * copy feedback, flash messages, etc.).
 *
 * `schedule` always cancels any pending timer first, so it is safe to call
 * repeatedly — only the most recent callback survives.
 */
export function useTimeout() {
    const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    const cancel = useCallback(() => {
        if (timerRef.current !== null) {
            clearTimeout(timerRef.current);
            timerRef.current = null;
        }
    }, []);

    const schedule = useCallback(
        (callback: () => void, ms: number) => {
            cancel();
            timerRef.current = setTimeout(() => {
                timerRef.current = null;
                callback();
            }, ms);
        },
        [cancel],
    );

    // Clear any pending timer on unmount — prevents setState-after-unmount.
    useEffect(() => cancel, [cancel]);

    return { schedule, cancel };
}
