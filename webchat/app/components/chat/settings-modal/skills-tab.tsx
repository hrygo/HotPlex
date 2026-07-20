"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import { createPortal } from "react-dom";
import type { Workspace } from "@/lib/api/workspaces";
import {
    listWorkspaceSkills,
    installWorkspaceSkill,
    deleteWorkspaceSkill,
    getWorkspaceSkill,
    type Skill,
    type SkillDetail,
} from "@/lib/api/skills";
import { TabPanel } from "./tab-panel";
import { useTranslation } from "react-i18next";

// Badge color helper — managed (writable) vs external (read-only) provenance.
function Badge({
    kind,
    label,
}: {
    kind: "managed" | "external";
    label: string;
}) {
    if (kind === "managed") {
        return (
            <span className="rounded-full bg-[rgba(16,185,129,0.12)] px-2 py-0.5 text-[10px] font-medium text-[rgb(16,185,129)]">
                {label}
            </span>
        );
    }
    return (
        <span className="rounded-full bg-[var(--bg-hover)] px-2 py-0.5 text-[10px] font-medium text-[var(--text-muted)]">
            {label}
        </span>
    );
}

interface SkillsTabProps {
    workspace: Workspace;
}

export function SkillsTab({ workspace }: SkillsTabProps) {
    const { t } = useTranslation(["chat", "common"]);
    const [skills, setSkills] = useState<Skill[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    // Status message for inline notifications (success, error, warning)
    const [statusMsg, setStatusMsg] = useState<{
        type: "success" | "error" | "warning";
        text: string;
    } | null>(null);
    const statusTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

    // Modals state
    const [showUpload, setShowUpload] = useState(false);
    const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

    // Detail dialog state (issue #918: workspace skill detail view).
    const [detailTarget, setDetailTarget] = useState<string | null>(null);
    const [detail, setDetail] = useState<SkillDetail | null>(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const [detailError, setDetailError] = useState<string | null>(null);

    // Action states
    const [uploading, setUploading] = useState(false);
    const [actionLoading, setActionLoading] = useState<string | null>(null);

    // Upload dialog form states
    const [file, setFile] = useState<File | null>(null);
    const [replace, setReplace] = useState(false);
    // In-dialog error for the upload flow. Rendered inside the modal so failures
    // are seen immediately — the top-of-tab status banner sits *behind* the open
    // modal overlay and would hide install errors from the user.
    const [uploadError, setUploadError] = useState<string | null>(null);
    const fileInputRef = useRef<HTMLInputElement>(null);

    const abortRef = useRef<AbortController | null>(null);
    const isMounted = useRef(false);

    // Pagination states
    const [currentPage, setCurrentPage] = useState(1);
    const pageSize = 5;

    const showStatus = (
        type: "success" | "error" | "warning",
        text: string,
    ) => {
        if (!isMounted.current) return;
        setStatusMsg({ type, text });
        if (statusTimer.current) clearTimeout(statusTimer.current);
        statusTimer.current = setTimeout(() => {
            if (isMounted.current) setStatusMsg(null);
        }, 5000);
    };

    const load = useCallback(async () => {
        abortRef.current?.abort();
        const ctrl = new AbortController();
        abortRef.current = ctrl;

        if (isMounted.current) {
            setLoading(true);
            setError(null);
        }

        try {
            // Workspace-only list (issue #918): this tab manages solely the skills
            // installed under the active workspace — no global/inherited entries.
            const res = await listWorkspaceSkills(workspace.id, ctrl.signal);
            if (ctrl.signal.aborted || !isMounted.current) return;
            setSkills(res.skills || []);
        } catch (err) {
            if (ctrl.signal.aborted || !isMounted.current) return;
            setError(err instanceof Error ? err.message : "Load failed");
        } finally {
            if (!ctrl.signal.aborted && isMounted.current) setLoading(false);
        }
    }, [workspace.id]);

    useEffect(() => {
        isMounted.current = true;
        // eslint-disable-next-line react-hooks/set-state-in-effect -- mount-time fetch
        load();
        return () => {
            isMounted.current = false;
            abortRef.current?.abort();
            if (statusTimer.current) clearTimeout(statusTimer.current);
        };
    }, [load]);

    const onPickFile = (f: File | null) => {
        if (f && !/\.zip$/i.test(f.name)) {
            // Surface the rejection inside the dialog — the top banner is hidden
            // behind the open modal overlay.
            setUploadError(
                t("settings.skills.error.not_zip", {
                    defaultValue: "Only .zip files are accepted",
                }),
            );
            return;
        }
        setUploadError(null);
        setFile(f);
    };

    const handleUpload = async () => {
        if (!file) {
            setUploadError(
                t("settings.skills.error.no_file", {
                    defaultValue: "Please select a zip file",
                }),
            );
            return;
        }
        setUploadError(null);
        try {
            setUploading(true);
            const res = await installWorkspaceSkill(
                workspace.id,
                file,
                replace,
            );
            await load();
            if (!isMounted.current) return;
            setShowUpload(false);
            setFile(null);
            setReplace(false);
            setUploadError(null);
            if (fileInputRef.current) fileInputRef.current.value = "";

            if (res.warning) {
                showStatus(
                    "warning",
                    t("settings.skills.toast.installed_warning", {
                        warning: res.warning,
                        defaultValue: `Installed — ${res.warning}`,
                    }),
                );
            } else {
                showStatus(
                    "success",
                    t("settings.skills.toast.installed", {
                        defaultValue: "Skill installed successfully",
                    }),
                );
            }
        } catch (err) {
            if (!isMounted.current) return;
            let errMsg = t("settings.skills.error.install_failed", {
                defaultValue: "Install failed",
            });
            if (err instanceof Error) {
                const msg = err.message;
                if (
                    msg.includes("SKILL_INVALID_ZIP") ||
                    msg.includes("invalid zip archive")
                ) {
                    errMsg = t("settings.skills.error.invalid_zip", {
                        defaultValue:
                            "Invalid zip archive. Please ensure the file is not corrupted.",
                    });
                } else if (
                    msg.includes("SKILL_FILE_TYPE_BLOCKED") ||
                    msg.includes("file type blocked")
                ) {
                    errMsg = t("settings.skills.error.file_type_blocked", {
                        defaultValue: "Contains blocked file types.",
                    });
                } else if (
                    msg.includes("SKILL_INVALID_FORMAT") ||
                    msg.includes("invalid format") ||
                    msg.includes("no SKILL.md found") ||
                    msg.includes("missing frontmatter") ||
                    msg.includes("no SKILL.md in")
                ) {
                    errMsg = t("settings.skills.error.invalid_format", {
                        defaultValue:
                            "Invalid skill format. Ensure root contains a valid SKILL.md file with YAML frontmatter.",
                    });
                } else if (
                    msg.includes("SKILL_ALREADY_EXISTS") ||
                    msg.includes("already exists")
                ) {
                    errMsg = t("settings.skills.error.already_exists", {
                        defaultValue:
                            'Skill already exists — enable "Replace existing" to overwrite.',
                    });
                } else if (
                    msg.includes("SKILL_NOT_FOUND") ||
                    msg.includes("not found")
                ) {
                    errMsg = t("settings.skills.error.not_found", {
                        defaultValue: "Skill not found.",
                    });
                } else {
                    // Only unexpected errors belong in the console. Known SKILL_*
                    // validation codes are user-input issues already surfaced in the
                    // dialog — logging them as console errors is just noise.
                    console.error(err);
                }
                // Any unmatched code (e.g. INTERNAL) or raw technical string must never
                // leak to the user — keep the friendly install_failed default instead.
            } else {
                console.error(err);
            }
            setUploadError(errMsg);
        } finally {
            if (isMounted.current) setUploading(false);
        }
    };

    const handleDelete = async (name: string) => {
        try {
            setActionLoading(name);
            await deleteWorkspaceSkill(workspace.id, name);
            await load();
            if (isMounted.current) {
                showStatus(
                    "success",
                    t("settings.skills.toast.deleted", {
                        defaultValue: "Skill deleted successfully",
                    }),
                );
            }
        } catch (err) {
            if (isMounted.current) {
                let errMsg = t("settings.skills.error.delete_failed", {
                    defaultValue: "Delete failed",
                });
                if (
                    err instanceof Error &&
                    (err.message.includes("SKILL_NOT_FOUND") ||
                        err.message.includes("not found"))
                ) {
                    errMsg = t("settings.skills.error.not_found", {
                        defaultValue: "Skill not found.",
                    });
                }
                // Unmatched codes fall back to the friendly delete_failed default — the
                // raw backend code/message is never surfaced.
                showStatus("error", errMsg);
            }
        } finally {
            if (isMounted.current) {
                setActionLoading(null);
                setDeleteTarget(null);
            }
        }
    };

    const closeUpload = () => {
        setShowUpload(false);
        setFile(null);
        setReplace(false);
        setUploadError(null);
        if (fileInputRef.current) fileInputRef.current.value = "";
    };

    // handleViewDetail opens the detail dialog and fetches the skill's SKILL.md
    // body + file list from the workspace-scoped detail endpoint (issue #918).
    const handleViewDetail = async (name: string) => {
        setDetailTarget(name);
        setDetail(null);
        setDetailError(null);
        setDetailLoading(true);
        try {
            const d = await getWorkspaceSkill(workspace.id, name);
            if (!isMounted.current) return;
            setDetail(d);
        } catch (err) {
            if (!isMounted.current) return;
            console.error(err);
            setDetailError(
                t("settings.skills.error.detail_failed", {
                    defaultValue: "Failed to load skill details",
                }),
            );
        } finally {
            if (isMounted.current) setDetailLoading(false);
        }
    };

    const closeDetail = () => {
        setDetailTarget(null);
        setDetail(null);
        setDetailError(null);
        setDetailLoading(false);
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center py-16">
                <div className="w-6 h-6 border-2 border-[var(--accent-gold)] stroke-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
            </div>
        );
    }

    if (error) {
        return (
            <div className="flex gap-2 px-4 py-3 rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] text-xs text-[var(--accent-coral)] font-bold items-center justify-center">
                <svg
                    className="w-4 h-4 shrink-0"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                >
                    <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        strokeWidth={2}
                        d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
                    />
                </svg>
                <span>
                    {t("common:status.error")}: {error}
                </span>
            </div>
        );
    }

    // Pagination — clamp the active page at render time so a shrinking list
    // (e.g. after a delete) never strands the view on a now-empty page. Derived
    // state, not an effect: an effect placed after the loading/error early
    // returns would violate the Rules of Hooks and crash the tab.
    const totalPages = Math.max(1, Math.ceil(skills.length / pageSize));
    const safePage = Math.min(currentPage, totalPages);
    const startIndex = (safePage - 1) * pageSize;
    const paginatedSkills = skills.slice(startIndex, startIndex + pageSize);

    return (
        <TabPanel>
            {/* Top Banner Status Notification */}
            {statusMsg && (
                <div
                    className={`flex gap-2 px-4 py-3 rounded-[var(--radius-md)] border text-xs font-bold items-start animate-fade-in-up ${
                        statusMsg.type === "success"
                            ? "bg-[rgba(52,211,153,0.08)] border-[rgba(52,211,153,0.15)] text-[var(--accent-emerald)]"
                            : statusMsg.type === "warning"
                              ? "bg-[rgba(251,191,36,0.08)] border-[rgba(251,191,36,0.15)] text-[var(--accent-gold)]"
                              : "bg-[rgba(244,63,94,0.08)] border-[rgba(244,63,94,0.15)] text-[var(--accent-coral)]"
                    }`}
                >
                    {statusMsg.type === "success" && (
                        <svg
                            className="w-4 h-4 shrink-0 mt-0.5"
                            fill="none"
                            stroke="currentColor"
                            viewBox="0 0 24 24"
                        >
                            <path
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                strokeWidth={2.5}
                                d="M5 13l4 4L19 7"
                            />
                        </svg>
                    )}
                    {(statusMsg.type === "warning" ||
                        statusMsg.type === "error") && (
                        <svg
                            className="w-4 h-4 shrink-0 mt-0.5"
                            fill="none"
                            stroke="currentColor"
                            viewBox="0 0 24 24"
                        >
                            <path
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                strokeWidth={2}
                                d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
                            />
                        </svg>
                    )}
                    <span className="break-words">{statusMsg.text}</span>
                </div>
            )}

            {/* Header and Upload Action wrapped in a section to align layout */}
            <section>
                <div className="flex items-center justify-between mb-3">
                    <h3 className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest">
                        {t("settings.skills.title.list", {
                            defaultValue: "Installed Skills",
                        })}
                    </h3>
                    <button
                        type="button"
                        onClick={() => setShowUpload(true)}
                        className="px-3 py-1.5 rounded-lg bg-[var(--accent-gold)] text-black text-[10px] font-bold hover:bg-[var(--accent-gold-bright)] transition-all cursor-pointer shadow-sm flex items-center gap-1 active:scale-[0.98]"
                    >
                        <svg
                            className="w-3 h-3"
                            fill="none"
                            stroke="currentColor"
                            viewBox="0 0 24 24"
                        >
                            <path
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                strokeWidth={2.5}
                                d="M12 4v16m8-8H4"
                            />
                        </svg>
                        {t("settings.skills.action.upload", {
                            defaultValue: "Upload Skill",
                        })}
                    </button>
                </div>

                {/* Skill List */}
                <div className="space-y-2">
                    {paginatedSkills.map((s) => {
                        // A skill is deletable if it is project-scoped (workspace) and managed.
                        const isDeletable = s.source === "project" && s.managed;

                        return (
                            <div
                                key={`${s.source}/${s.name}`}
                                className="flex items-center justify-between rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-3.5 hover:border-[var(--border-default)] transition-colors"
                            >
                                <div className="min-w-0 flex-1">
                                    <div className="flex items-center gap-2 flex-wrap">
                                        <span className="truncate text-sm font-bold text-[var(--text-primary)]">
                                            {s.name}
                                        </span>
                                        <Badge
                                            kind={
                                                s.managed
                                                    ? "managed"
                                                    : "external"
                                            }
                                            label={
                                                s.managed
                                                    ? t(
                                                          "settings.skills.label.managed",
                                                      )
                                                    : t(
                                                          "settings.skills.label.external",
                                                      )
                                            }
                                        />
                                        <span className="text-[9px] font-mono font-bold uppercase px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] text-[var(--text-muted)] border border-[var(--border-subtle)]">
                                            {s.source === "project"
                                                ? t(
                                                      "chat:settings.label.active_workspace",
                                                  )
                                                : t(
                                                      "chat:settings.group.personal",
                                                  )}
                                        </span>
                                    </div>
                                    <p className="mt-1 text-xs text-[var(--text-muted)] line-clamp-2 leading-relaxed">
                                        {s.description}
                                    </p>
                                </div>
                                <div className="ml-3 flex shrink-0 items-center gap-2">
                                    <button
                                        type="button"
                                        onClick={() => handleViewDetail(s.name)}
                                        className="rounded-md border border-[var(--border-subtle)] px-2.5 py-1.5 text-[10px] font-bold text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:border-[var(--border-default)] transition-all cursor-pointer active:scale-[0.98]"
                                    >
                                        {t("settings.skills.action.details", {
                                            defaultValue: "Details",
                                        })}
                                    </button>
                                    {isDeletable && (
                                        <button
                                            type="button"
                                            disabled={actionLoading === s.name}
                                            onClick={() => setDeleteTarget(s.name)}
                                            className="rounded-md border border-[var(--border-subtle)] px-2.5 py-1.5 text-[10px] font-bold text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.08)] hover:border-[var(--accent-coral)]/30 disabled:opacity-50 transition-all cursor-pointer active:scale-[0.98]"
                                        >
                                            {t("settings.skills.action.delete", {
                                                defaultValue: "Delete",
                                            })}
                                        </button>
                                    )}
                                </div>
                            </div>
                        );
                    })}
                    {skills.length === 0 && (
                        <div className="flex flex-col items-center justify-center py-16 text-center border border-dashed border-[var(--border-subtle)] rounded-lg bg-[var(--bg-elevated)]/10">
                            <p className="text-sm font-medium text-[var(--text-secondary)] mb-1">
                                {t("settings.skills.empty.title", {
                                    defaultValue:
                                        "No skills installed in this workspace",
                                })}
                            </p>
                            <p className="text-xs text-[var(--text-faint)] max-w-sm">
                                {t("settings.skills.empty.desc", {
                                    defaultValue:
                                        "Upload a skill .zip to install it into this workspace's skills directory.",
                                })}
                            </p>
                        </div>
                    )}
                </div>

                {/* Pagination Controls */}
                {totalPages > 1 && (
                    <div className="flex items-center justify-between pt-4 border-t border-[var(--border-subtle)] mt-4">
                        <button
                            type="button"
                            disabled={safePage === 1}
                            onClick={() =>
                                setCurrentPage(Math.max(1, safePage - 1))
                            }
                            className="px-2.5 py-1.5 rounded-md border border-[var(--border-subtle)] text-[10px] font-bold text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] disabled:opacity-40 disabled:cursor-not-allowed transition-all cursor-pointer"
                        >
                            {t("settings.skills.pagination.prev", {
                                defaultValue: "Previous",
                            })}
                        </button>
                        <span className="text-[10px] font-mono text-[var(--text-muted)] font-bold">
                            {safePage} / {totalPages}
                        </span>
                        <button
                            type="button"
                            disabled={safePage === totalPages}
                            onClick={() =>
                                setCurrentPage(
                                    Math.min(totalPages, safePage + 1),
                                )
                            }
                            className="px-2.5 py-1.5 rounded-md border border-[var(--border-subtle)] text-[10px] font-bold text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] disabled:opacity-40 disabled:cursor-not-allowed transition-all cursor-pointer"
                        >
                            {t("settings.skills.pagination.next", {
                                defaultValue: "Next",
                            })}
                        </button>
                    </div>
                )}
            </section>

            {/* Upload Dialog Modal Overlay — portaled to <body>: the settings content
          card has `overflow-hidden backdrop-blur-md`, and backdrop-filter makes
          it the containing block for `fixed` descendants, so an in-place overlay
          would be positioned/clipped to the card instead of the viewport. */}
            {showUpload &&
                createPortal(
                    <div
                        className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm"
                        onClick={closeUpload}
                    >
                        <div
                            className="relative w-full max-w-md border border-[var(--border-default)] bg-[var(--bg-elevated)] p-6 rounded-xl shadow-2xl"
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="mb-4 flex items-center justify-between">
                                <h2 className="text-sm font-bold text-[var(--text-primary)]">
                                    {t("settings.skills.dialog.upload_title", {
                                        defaultValue: "Upload Workspace Skill",
                                    })}
                                </h2>
                                <button
                                    onClick={closeUpload}
                                    className="text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors cursor-pointer"
                                    aria-label={t("common:action.close")}
                                >
                                    <svg
                                        className="w-4 h-4"
                                        fill="none"
                                        stroke="currentColor"
                                        viewBox="0 0 24 24"
                                    >
                                        <path
                                            strokeLinecap="round"
                                            strokeLinejoin="round"
                                            strokeWidth={2}
                                            d="M6 18L18 6M6 6l12 12"
                                        />
                                    </svg>
                                </button>
                            </div>

                            <p className="text-xs text-[var(--text-muted)] leading-relaxed">
                                {t("settings.skills.dialog.upload_desc", {
                                    defaultValue:
                                        "Select a .zip whose root has SKILL.md (or a single top-level dir containing it). Aligned with agentskills.io.",
                                })}
                            </p>

                            <div className="mt-4">
                                <input
                                    ref={fileInputRef}
                                    type="file"
                                    accept=".zip"
                                    onChange={(e) =>
                                        onPickFile(e.target.files?.[0] ?? null)
                                    }
                                    className="hidden"
                                />
                                <div className="flex items-center gap-3">
                                    <button
                                        type="button"
                                        onClick={() =>
                                            fileInputRef.current?.click()
                                        }
                                        className="rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-hover)] px-3 py-2 text-[10px] font-bold text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:border-[var(--border-default)] transition-all cursor-pointer"
                                    >
                                        {t(
                                            "settings.skills.action.choose_file",
                                            { defaultValue: "Choose File" },
                                        )}
                                    </button>
                                    <span className="text-[10px] font-mono text-[var(--text-secondary)] truncate flex-1">
                                        {file
                                            ? file.name
                                            : t(
                                                  "settings.skills.placeholder.no_file",
                                                  {
                                                      defaultValue:
                                                          "No file chosen (.zip)",
                                                  },
                                              )}
                                    </span>
                                </div>
                            </div>

                            <label className="mt-4 flex items-center gap-2 text-xs text-[var(--text-secondary)] cursor-pointer select-none">
                                <input
                                    type="checkbox"
                                    checked={replace}
                                    onChange={(e) =>
                                        setReplace(e.target.checked)
                                    }
                                    className="rounded border-[var(--border-default)] bg-[var(--bg-elevated)] text-[var(--accent-gold)] focus:ring-[var(--accent-gold)]/20"
                                />
                                {t("settings.skills.dialog.replace", {
                                    defaultValue:
                                        "Replace existing same-name skill",
                                })}
                            </label>

                            {/* Inline install error — shown here (not the top banner) so it is
                visible while the dialog is open. */}
                            {uploadError && (
                                <div
                                    role="alert"
                                    className="mt-4 flex items-start gap-2 rounded-lg border border-[rgba(244,63,94,0.25)] bg-[rgba(244,63,94,0.08)] px-3 py-2.5 text-xs font-medium text-[var(--accent-coral)] animate-fade-in-up"
                                >
                                    <svg
                                        className="w-4 h-4 shrink-0 mt-0.5"
                                        fill="none"
                                        stroke="currentColor"
                                        viewBox="0 0 24 24"
                                    >
                                        <path
                                            strokeLinecap="round"
                                            strokeLinejoin="round"
                                            strokeWidth={2}
                                            d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
                                        />
                                    </svg>
                                    <span className="min-w-0 break-words">
                                        {uploadError}
                                    </span>
                                </div>
                            )}

                            <div className="mt-6 flex justify-end gap-3">
                                <button
                                    type="button"
                                    onClick={closeUpload}
                                    className="px-4 py-2 text-xs font-medium rounded-md border border-[var(--border-default)] bg-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-all cursor-pointer"
                                >
                                    {t("common:action.cancel", {
                                        defaultValue: "Cancel",
                                    })}
                                </button>
                                <button
                                    type="button"
                                    onClick={handleUpload}
                                    disabled={uploading || !file}
                                    className="px-4 py-2 text-xs font-semibold rounded-md bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-all disabled:opacity-40 disabled:cursor-not-allowed shadow-sm flex items-center gap-1.5 cursor-pointer"
                                >
                                    {uploading && (
                                        <div className="w-3 h-3 border-2 border-t-transparent border-black rounded-full animate-spin" />
                                    )}
                                    {uploading
                                        ? t(
                                              "settings.skills.dialog.uploading",
                                              { defaultValue: "Uploading…" },
                                          )
                                        : t("settings.skills.action.install", {
                                              defaultValue: "Install",
                                          })}
                                </button>
                            </div>
                        </div>
                    </div>,
                    document.body,
                )}

            {/* Delete Confirmation Modal Overlay — portaled for the same reason. */}
            {deleteTarget &&
                createPortal(
                    <div
                        className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm"
                        onClick={() => setDeleteTarget(null)}
                    >
                        <div
                            className="relative w-full max-w-md border border-[var(--border-default)] bg-[var(--bg-elevated)] p-6 rounded-xl shadow-2xl"
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="flex items-start gap-4">
                                {/* Warning Indicator */}
                                <div className="flex-shrink-0 w-10 h-10 rounded-full flex items-center justify-center bg-white/5 border border-white/10">
                                    <svg
                                        className="w-5 h-5 text-[var(--accent-coral)]"
                                        fill="none"
                                        viewBox="0 0 24 24"
                                        stroke="currentColor"
                                    >
                                        <path
                                            strokeLinecap="round"
                                            strokeLinejoin="round"
                                            strokeWidth={2}
                                            d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
                                        />
                                    </svg>
                                </div>

                                <div className="flex-1 min-w-0">
                                    <h3 className="text-base font-display font-bold text-[var(--text-primary)]">
                                        {t(
                                            "settings.skills.confirm.delete_title",
                                            {
                                                defaultValue:
                                                    "Delete Workspace Skill",
                                            },
                                        )}
                                    </h3>
                                    <p className="mt-2 text-sm text-[var(--text-secondary)] leading-relaxed">
                                        {t(
                                            "settings.skills.confirm.delete_body",
                                            {
                                                name: deleteTarget,
                                                defaultValue: `Delete skill "${deleteTarget}"? This removes it from the workspace skills directory and cannot be undone.`,
                                            },
                                        )}
                                    </p>
                                </div>
                            </div>

                            {/* Action Buttons */}
                            <div className="mt-6 flex justify-end gap-3">
                                <button
                                    type="button"
                                    onClick={() => setDeleteTarget(null)}
                                    className="px-4 py-2 text-xs font-medium rounded-md border border-[var(--border-default)] bg-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-all cursor-pointer"
                                >
                                    {t("common:action.cancel", {
                                        defaultValue: "Cancel",
                                    })}
                                </button>
                                <button
                                    type="button"
                                    onClick={() => handleDelete(deleteTarget)}
                                    className="px-4 py-2 text-xs font-semibold rounded-md bg-[var(--accent-coral)] text-white hover:bg-[var(--accent-coral)]/90 transition-all shadow-sm cursor-pointer"
                                >
                                    {t("common:action.delete", {
                                        defaultValue: "Delete",
                                    })}
                                </button>
                            </div>
                        </div>
                    </div>,
                    document.body,
                )}

            {/* Skill Detail Dialog (issue #918) — portaled for the same reason as
          the other dialogs (settings card overflow-hidden + backdrop-filter). */}
            {detailTarget &&
                createPortal(
                    <div
                        className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm"
                        onClick={closeDetail}
                    >
                        <div
                            className="relative w-full max-w-lg max-h-[80vh] flex flex-col border border-[var(--border-default)] bg-[var(--bg-elevated)] p-6 rounded-xl shadow-2xl"
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="mb-4 flex items-center justify-between">
                                <h2 className="text-sm font-bold text-[var(--text-primary)]">
                                    {t("settings.skills.dialog.detail_title", {
                                        defaultValue: "Skill Details",
                                    })}
                                </h2>
                                <button
                                    onClick={closeDetail}
                                    className="text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors cursor-pointer"
                                    aria-label={t("common:action.close")}
                                >
                                    <svg
                                        className="w-4 h-4"
                                        fill="none"
                                        stroke="currentColor"
                                        viewBox="0 0 24 24"
                                    >
                                        <path
                                            strokeLinecap="round"
                                            strokeLinejoin="round"
                                            strokeWidth={2}
                                            d="M6 18L18 6M6 6l12 12"
                                        />
                                    </svg>
                                </button>
                            </div>

                            <div className="mb-3 text-xs font-mono font-bold text-[var(--accent-gold)]">
                                {detailTarget}
                            </div>

                            {detailLoading && (
                                <div className="flex items-center justify-center py-10">
                                    <div className="w-5 h-5 border-2 border-[var(--accent-gold)] stroke-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
                                </div>
                            )}

                            {!detailLoading && detailError && (
                                <div
                                    role="alert"
                                    className="flex items-start gap-2 rounded-lg border border-[rgba(244,63,94,0.25)] bg-[rgba(244,63,94,0.08)] px-3 py-2.5 text-xs font-medium text-[var(--accent-coral)]"
                                >
                                    <span className="min-w-0 break-words">
                                        {detailError}
                                    </span>
                                </div>
                            )}

                            {!detailLoading && detail && (
                                <div className="min-h-0 flex-1 space-y-4 overflow-y-auto">
                                    {detail.description && (
                                        <p className="text-xs text-[var(--text-muted)] leading-relaxed">
                                            {detail.description}
                                        </p>
                                    )}
                                    {detail.files && detail.files.length > 0 && (
                                        <div>
                                            <h3 className="mb-1.5 text-[10px] font-mono font-bold uppercase tracking-widest text-[var(--text-faint)]">
                                                {t(
                                                    "settings.skills.dialog.detail_files",
                                                    { defaultValue: "Files" },
                                                )}
                                            </h3>
                                            <ul className="space-y-1">
                                                {detail.files.map((f) => (
                                                    <li
                                                        key={f}
                                                        className="text-[10px] font-mono text-[var(--text-secondary)]"
                                                    >
                                                        {f}
                                                    </li>
                                                ))}
                                            </ul>
                                        </div>
                                    )}
                                    {detail.body && (
                                        <div>
                                            <h3 className="mb-1.5 text-[10px] font-mono font-bold uppercase tracking-widest text-[var(--text-faint)]">
                                                {t(
                                                    "settings.skills.dialog.detail_body",
                                                    {
                                                        defaultValue:
                                                            "SKILL.md",
                                                    },
                                                )}
                                            </h3>
                                            <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-3 text-[10px] leading-relaxed text-[var(--text-secondary)]">
                                                {detail.body}
                                            </pre>
                                        </div>
                                    )}
                                </div>
                            )}
                        </div>
                    </div>,
                    document.body,
                )}
        </TabPanel>
    );
}
