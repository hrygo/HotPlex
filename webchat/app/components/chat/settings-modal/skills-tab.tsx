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
import { useResource } from "@/hooks/use-resource";

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
            <span className="inline-flex items-center rounded-full bg-[rgba(16,185,129,0.1)] border border-[rgba(16,185,129,0.25)] px-2 py-0.5 text-[10px] font-mono font-bold uppercase tracking-wider text-[rgb(16,185,129)]">
                {label}
            </span>
        );
    }
    return (
        <span className="inline-flex items-center rounded-full bg-[var(--bg-hover)] border border-[var(--border-subtle)] px-2 py-0.5 text-[10px] font-mono font-bold uppercase tracking-wider text-[var(--text-muted)]">
            {label}
        </span>
    );
}

interface SkillsTabProps {
    workspace: Workspace;
}

export function SkillsTab({ workspace }: SkillsTabProps) {
    const { t } = useTranslation(["chat", "common"]);
    const { data: skillsData, loading, error, reload } = useResource<Skill[]>(
        async () => (await listWorkspaceSkills(workspace.id)).skills ?? [],
        [workspace.id],
    );
    const skills = skillsData ?? [];

    // Search and Pagination States
    const [search, setSearch] = useState("");
    const [currentPage, setCurrentPage] = useState(1);
    const [pageSize, setPageSize] = useState(5);

    // Status message for inline notifications (success, error, warning)
    const [statusMsg, setStatusMsg] = useState<{
        type: "success" | "error" | "warning";
        text: string;
    } | null>(null);
    const statusTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

    // Modals state
    const [showUpload, setShowUpload] = useState(false);
    const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

    // Detail dialog state
    const [detailTarget, setDetailTarget] = useState<string | null>(null);
    const [detail, setDetail] = useState<SkillDetail | null>(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const [detailError, setDetailError] = useState<string | null>(null);
    const [activeTab, setActiveTab] = useState<"body" | "files">("body");
    const [prevWorkspaceId, setPrevWorkspaceId] = useState(workspace.id);

    // Action states
    const [uploading, setUploading] = useState(false);
    const [actionLoading, setActionLoading] = useState<string | null>(null);

    // Upload dialog form states
    const [file, setFile] = useState<File | null>(null);
    const [replace, setReplace] = useState(false);
    const [uploadError, setUploadError] = useState<string | null>(null);
    const fileInputRef = useRef<HTMLInputElement>(null);

    const isMounted = useRef(false);
    const detailAbortRef = useRef<AbortController | null>(null);

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

    useEffect(() => {
        isMounted.current = true;
        return () => {
            isMounted.current = false;
            if (statusTimer.current) clearTimeout(statusTimer.current);
        };
    }, []);

    const onPickFile = (f: File | null) => {
        if (f && !/\.zip$/i.test(f.name)) {
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
            await reload();
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
                const msg = err.message || "";
                const extMatch = msg.match(/\.([a-zA-Z0-9]+)/);
                const ext = extMatch ? `.${extMatch[1].toLowerCase()}` : "";

                if (
                    msg.includes("SKILL_FILE_TYPE_BLOCKED") ||
                    msg.includes("blocked file type") ||
                    msg.includes("file type blocked")
                ) {
                    errMsg = ext
                        ? t("admin:skills.error.blocked_ext", {
                              ext,
                              defaultValue: `Zip contains unsupported file type (${ext}). Only .md, .json, .yaml, .txt, .png, .jpg, .py, .sh are allowed.`,
                          })
                        : t("admin:skills.error.file_type_blocked", {
                              defaultValue:
                                  "Zip contains blocked file types (e.g. .pptx, .exe, .docx). Only text and asset files are allowed.",
                          });
                } else if (
                    msg.includes("SKILL_INVALID_ZIP") ||
                    msg.includes("invalid zip") ||
                    msg.includes("invalid, corrupt, or oversized zip")
                ) {
                    errMsg = t("admin:skills.error.invalid_zip", {
                        defaultValue:
                            "Invalid zip archive. Please ensure the file is not corrupted.",
                    });
                } else if (
                    msg.includes("SKILL_INVALID_FORMAT") ||
                    msg.includes("invalid format") ||
                    msg.includes("no SKILL.md found") ||
                    msg.includes("missing frontmatter") ||
                    msg.includes("no SKILL.md in")
                ) {
                    errMsg = t("admin:skills.error.invalid_format", {
                        defaultValue:
                            "Invalid skill format. Ensure root contains a valid SKILL.md file with YAML frontmatter.",
                    });
                } else if (
                    msg.includes("SKILL_ALREADY_EXISTS") ||
                    msg.includes("already exists")
                ) {
                    errMsg = t("admin:skills.error.already_exists", {
                        defaultValue:
                            'Skill already exists — enable "Replace existing" to overwrite.',
                    });
                } else if (
                    msg.includes("SKILL_NOT_FOUND") ||
                    msg.includes("not found")
                ) {
                    errMsg = t("admin:skills.error.not_found", {
                        defaultValue: "Skill not found.",
                    });
                } else {
                    const cleaned = msg
                        .replace(/\\x[0-9a-fA-F]{2}/g, "")
                        .replace(/^skill:\s*/i, "")
                        .trim();
                    if (cleaned) errMsg = cleaned;
                }
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
            await reload();
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

    const handleViewDetail = async (name: string) => {
        detailAbortRef.current?.abort();
        const ctrl = new AbortController();
        detailAbortRef.current = ctrl;

        setDetailTarget(name);
        setDetail(null);
        setDetailError(null);
        setDetailLoading(true);
        setActiveTab("body");
        try {
            const d = await getWorkspaceSkill(workspace.id, name, ctrl.signal);
            if (ctrl.signal.aborted || !isMounted.current) return;
            setDetail(d);
        } catch (err) {
            if (ctrl.signal.aborted || !isMounted.current) return;
            const msg = err instanceof Error ? err.message : "";
            if (!msg.includes("SKILL_NOT_FOUND") && !msg.includes("not found")) {
                console.error(err);
            }
            setDetailError(
                t("settings.skills.error.detail_failed", {
                    defaultValue: "Failed to load skill details",
                }),
            );
        } finally {
            if (!ctrl.signal.aborted && isMounted.current) setDetailLoading(false);
        }
    };

    const closeDetail = useCallback(() => {
        detailAbortRef.current?.abort();
        setDetailTarget(null);
        setDetail(null);
        setDetailError(null);
        setDetailLoading(false);
    }, []);

    useEffect(() => {
        return () => {
            detailAbortRef.current?.abort();
        };
    }, [workspace.id]);

    if (workspace.id !== prevWorkspaceId) {
        setPrevWorkspaceId(workspace.id);
        setDetailTarget(null);
        setDetail(null);
        setDetailError(null);
        setDetailLoading(false);
    }

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

    // Filter skills by search query
    const filteredSkills = skills.filter((s) => {
        if (!search.trim()) return true;
        const q = search.toLowerCase();
        return (
            s.name.toLowerCase().includes(q) ||
            (s.description && s.description.toLowerCase().includes(q))
        );
    });

    const totalPages = Math.max(1, Math.ceil(filteredSkills.length / pageSize));
    const safePage = Math.min(currentPage, totalPages);
    const startIndex = (safePage - 1) * pageSize;
    const paginatedSkills = filteredSkills.slice(
        startIndex,
        startIndex + pageSize,
    );

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

            {/* Header and Upload Action wrapped in a section */}
            <section className="space-y-4">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                    <div>
                        <div className="flex items-center gap-2">
                            <h3 className="text-base font-display font-bold text-[var(--text-primary)]">
                                {t("settings.skills.title.list", {
                                    defaultValue: "Installed Skills",
                                })}
                            </h3>
                            <span className="text-[11px] font-mono font-bold text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)]">
                                {skills.length}
                            </span>
                        </div>
                        <p className="mt-0.5 text-xs text-[var(--text-muted)]">
                            Manage skills installed specifically in this workspace.
                        </p>
                    </div>

                    <button
                        type="button"
                        onClick={() => setShowUpload(true)}
                        className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-all active:scale-95 cursor-pointer shadow-sm"
                    >
                        <svg
                            className="w-3.5 h-3.5"
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

                {/* Search Bar */}
                <div className="relative">
                    <input
                        type="text"
                        value={search}
                        onChange={(e) => {
                            setSearch(e.target.value);
                            setCurrentPage(1);
                        }}
                        placeholder="Search workspace skill name or description…"
                        className="w-full rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3.5 py-2 pl-9 text-xs text-[var(--text-primary)] placeholder-[var(--text-muted)] focus:border-[var(--accent-gold)] focus:outline-none transition-colors"
                    />
                    <svg
                        className="absolute left-3 top-2.5 h-4 w-4 text-[var(--text-muted)] pointer-events-none"
                        fill="none"
                        stroke="currentColor"
                        viewBox="0 0 24 24"
                    >
                        <path
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            strokeWidth={2}
                            d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
                        />
                    </svg>
                    {search && (
                        <button
                            onClick={() => {
                                setSearch("");
                                setCurrentPage(1);
                            }}
                            className="absolute right-3 top-2.5 text-xs font-bold text-[var(--text-muted)] hover:text-[var(--text-primary)]"
                        >
                            ✕
                        </button>
                    )}
                </div>

                {/* Skill List Card Panel */}
                <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden shadow-sm">
                    <div className="divide-y divide-[var(--border-subtle)]">
                        {paginatedSkills.map((s) => (
                            <div
                                key={s.name}
                                className="flex items-center justify-between px-4 py-2.5 transition-colors hover:bg-[var(--bg-hover)]"
                            >
                                <div
                                    className="min-w-0 flex-1 cursor-pointer pr-4"
                                    onClick={() => handleViewDetail(s.name)}
                                >
                                    <div className="flex items-center gap-2 mb-0.5">
                                        <span className="truncate text-xs font-bold text-[var(--text-primary)] hover:text-[var(--accent-gold)] transition-colors">
                                            {s.name}
                                        </span>
                                        <Badge
                                            kind="managed"
                                            label={t(
                                                "settings.skills.label.managed",
                                                { defaultValue: "Managed" },
                                            )}
                                        />
                                        <span className="text-[10px] font-mono font-bold uppercase tracking-wider text-[var(--text-faint)] bg-[var(--bg-hover)] px-1.5 py-0.5 rounded border border-[var(--border-subtle)]">
                                            workspace
                                        </span>
                                    </div>
                                    <p className="line-clamp-1 text-xs text-[var(--text-muted)] leading-relaxed">
                                        {s.description}
                                    </p>
                                </div>
                                <div className="flex shrink-0 items-center gap-2">
                                    <button
                                        type="button"
                                        onClick={() => handleViewDetail(s.name)}
                                        className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-2.5 py-1 text-xs font-semibold text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-colors active:scale-95"
                                    >
                                        {t("settings.skills.action.details", {
                                            defaultValue: "Details",
                                        })}
                                    </button>
                                    <button
                                        type="button"
                                        disabled={actionLoading === s.name}
                                        onClick={() => setDeleteTarget(s.name)}
                                        className="rounded-[var(--radius-sm)] border border-[rgba(244,63,94,0.2)] px-2.5 py-1 text-xs font-semibold text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.08)] transition-colors disabled:opacity-50 active:scale-95"
                                    >
                                        {t("settings.skills.action.delete", {
                                            defaultValue: "Delete",
                                        })}
                                    </button>
                                </div>
                            </div>
                        ))}
                    </div>

                    {filteredSkills.length === 0 && (
                        <div className="flex flex-col items-center justify-center py-12 text-center p-6">
                            <p className="text-xs font-bold text-[var(--text-secondary)] mb-1">
                                {t("settings.skills.empty.title", {
                                    defaultValue:
                                        "No skills installed in this workspace",
                                })}
                            </p>
                            <p className="text-xs text-[var(--text-muted)] max-w-sm">
                                {t("settings.skills.empty.desc", {
                                    defaultValue:
                                        "Upload a skill .zip to install it into this workspace's skills directory.",
                                })}
                            </p>
                        </div>
                    )}

                    {/* Pagination Footer */}
                    <div className="flex items-center justify-between border-t border-[var(--border-subtle)] px-4 py-2.5 bg-[var(--bg-surface)] text-xs text-[var(--text-muted)]">
                        <span className="font-mono text-[11px]">
                            Page {safePage} of {totalPages} (Total {filteredSkills.length})
                        </span>
                        <div className="flex items-center gap-3">
                            <select
                                value={pageSize}
                                onChange={(e) => {
                                    setPageSize(Number(e.target.value));
                                    setCurrentPage(1);
                                }}
                                className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-hover)] px-2 py-1 text-[11px] font-mono text-[var(--text-secondary)] focus:outline-none cursor-pointer"
                            >
                                <option value={5}>5 / page</option>
                                <option value={10}>10 / page</option>
                                <option value={20}>20 / page</option>
                            </select>
                            <div className="flex items-center gap-1.5">
                                <button
                                    type="button"
                                    disabled={safePage <= 1}
                                    onClick={() =>
                                        setCurrentPage(Math.max(1, safePage - 1))
                                    }
                                    className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-2.5 py-1 text-[11px] font-semibold text-[var(--text-primary)] hover:bg-[var(--bg-hover)] disabled:opacity-40 transition-colors"
                                >
                                    {t("settings.skills.pagination.prev", {
                                        defaultValue: "Previous",
                                    })}
                                </button>
                                <button
                                    type="button"
                                    disabled={safePage >= totalPages}
                                    onClick={() =>
                                        setCurrentPage(
                                            Math.min(totalPages, safePage + 1),
                                        )
                                    }
                                    className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-2.5 py-1 text-[11px] font-semibold text-[var(--text-primary)] hover:bg-[var(--bg-hover)] disabled:opacity-40 transition-colors"
                                >
                                    {t("settings.skills.pagination.next", {
                                        defaultValue: "Next",
                                    })}
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            {/* Upload Dialog Modal Overlay */}
            {showUpload &&
                createPortal(
                    <div
                        className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm"
                        onClick={closeUpload}
                    >
                        <div
                            className="w-full max-w-lg sm:max-w-xl max-h-[85vh] flex flex-col rounded-xl bg-[var(--bg-surface)] p-6 shadow-2xl border border-[var(--border-subtle)] transition-all"
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="flex items-start justify-between border-b border-[var(--border-subtle)] pb-4">
                                <div>
                                    <h2 className="text-base font-display font-bold text-[var(--text-primary)]">
                                        {t("settings.skills.dialog.upload_title", {
                                            defaultValue: "Upload Workspace Skill",
                                        })}
                                    </h2>
                                    <p className="mt-1 text-xs text-[var(--text-muted)] leading-relaxed">
                                        {t("settings.skills.dialog.upload_desc", {
                                            defaultValue:
                                                "Select a .zip whose root has SKILL.md (or a single top-level dir containing it). Aligned with agentskills.io.",
                                        })}
                                    </p>
                                </div>
                                <button
                                    onClick={closeUpload}
                                    className="rounded-full p-1.5 text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all"
                                >
                                    ✕
                                </button>
                            </div>

                            <div className="mt-5 space-y-4 flex-1 overflow-y-auto pr-0.5">
                                <div>
                                    <input
                                        ref={fileInputRef}
                                        type="file"
                                        accept=".zip"
                                        onChange={(e) =>
                                            onPickFile(e.target.files?.[0] ?? null)
                                        }
                                        className="block w-full text-xs text-[var(--text-muted)] file:mr-3 file:rounded-md file:border-0 file:bg-[var(--accent-gold)] file:px-3.5 file:py-2 file:text-xs file:font-bold file:text-black hover:file:bg-[var(--accent-gold-bright)] transition-colors cursor-pointer"
                                    />
                                    {file && (
                                        <p className="mt-2 text-xs font-mono text-[var(--text-secondary)]">
                                            {file.name}
                                        </p>
                                    )}
                                </div>

                                <label className="flex items-center gap-2 text-xs font-medium text-[var(--text-secondary)] cursor-pointer select-none">
                                    <input
                                        type="checkbox"
                                        checked={replace}
                                        onChange={(e) =>
                                            setReplace(e.target.checked)
                                        }
                                        className="rounded accent-[var(--accent-gold)]"
                                    />
                                    {t("settings.skills.dialog.replace", {
                                        defaultValue:
                                            "Replace existing same-name skill",
                                    })}
                                </label>

                                {uploadError && (
                                    <div
                                        role="alert"
                                        className="flex items-start gap-2 rounded-lg border border-[rgba(244,63,94,0.25)] bg-[rgba(244,63,94,0.08)] px-3 py-2.5 text-xs font-medium text-[var(--accent-coral)] animate-fade-in-up"
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
                            </div>

                            <div className="mt-6 flex justify-end gap-2 border-t border-[var(--border-subtle)] pt-4">
                                <button
                                    type="button"
                                    onClick={closeUpload}
                                    className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-4 py-1.5 text-xs font-semibold text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-colors"
                                >
                                    {t("common:action.cancel", {
                                        defaultValue: "Cancel",
                                    })}
                                </button>
                                <button
                                    type="button"
                                    onClick={handleUpload}
                                    disabled={uploading || !file}
                                    className="rounded-[var(--radius-sm)] bg-[var(--accent-gold)] px-4 py-1.5 text-xs font-bold uppercase tracking-wider text-black hover:bg-[var(--accent-gold-bright)] active:scale-95 transition-all disabled:opacity-50"
                                >
                                    {uploading && (
                                        <div className="w-3.5 h-3.5 border-2 border-t-transparent border-black rounded-full animate-spin" />
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

            {/* Delete Confirmation Modal Overlay */}
            {deleteTarget &&
                createPortal(
                    <div
                        className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm"
                        onClick={() => setDeleteTarget(null)}
                    >
                        <div
                            className="w-full max-w-md rounded-xl bg-[var(--bg-surface)] p-6 shadow-2xl border border-[var(--border-subtle)]"
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="flex items-start gap-4">
                                <div className="flex-shrink-0 w-10 h-10 rounded-full flex items-center justify-center bg-[rgba(244,63,94,0.1)] border border-[rgba(244,63,94,0.2)]">
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
                                    <p className="mt-2 text-xs text-[var(--text-muted)] leading-relaxed">
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

                            <div className="mt-6 flex justify-end gap-2 border-t border-[var(--border-subtle)] pt-4">
                                <button
                                    type="button"
                                    onClick={() => setDeleteTarget(null)}
                                    className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-4 py-1.5 text-xs font-semibold text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all"
                                >
                                    {t("common:action.cancel", {
                                        defaultValue: "Cancel",
                                    })}
                                </button>
                                <button
                                    type="button"
                                    onClick={() => handleDelete(deleteTarget)}
                                    className="rounded-[var(--radius-sm)] bg-[var(--accent-coral)] px-4 py-1.5 text-xs font-bold uppercase tracking-wider text-white hover:bg-[var(--accent-coral)]/90 active:scale-95 transition-all shadow-sm"
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

            {/* Skill Detail Dialog Overlay */}
            {detailTarget &&
                createPortal(
                    <div
                        className="fixed inset-0 z-50 flex items-center justify-center p-4 sm:p-6 bg-black/60 backdrop-blur-sm"
                        onClick={closeDetail}
                    >
                        <div
                            className="flex max-h-[90vh] sm:h-[85vh] w-full max-w-2xl sm:max-w-4xl lg:max-w-5xl flex-col rounded-xl bg-[var(--bg-surface)] p-6 shadow-2xl border border-[var(--border-subtle)] transition-all"
                            onClick={(e) => e.stopPropagation()}
                        >
                            {/* Modal Header */}
                            <div className="flex items-start justify-between border-b border-[var(--border-subtle)] pb-4">
                                <div className="min-w-0 flex-1 pr-4">
                                    <div className="flex flex-wrap items-center gap-2 mb-1">
                                        <h2 className="text-lg font-display font-bold text-[var(--text-primary)]">
                                            {detailTarget}
                                        </h2>
                                        <Badge kind="managed" label="Managed" />
                                        <span className="text-[10px] font-mono font-bold uppercase tracking-wider text-[var(--text-faint)] bg-[var(--bg-hover)] px-2 py-0.5 rounded border border-[var(--border-subtle)]">
                                            workspace
                                        </span>
                                    </div>
                                    {detail?.description && (
                                        <p className="text-xs text-[var(--text-muted)] leading-relaxed">
                                            {detail.description}
                                        </p>
                                    )}
                                </div>

                                <div className="flex shrink-0 items-center gap-2">
                                    {detail?.body && (
                                        <button
                                            type="button"
                                            onClick={async () => {
                                                try {
                                                    await navigator.clipboard.writeText(
                                                        detail.body || "",
                                                    );
                                                    showStatus(
                                                        "success",
                                                        t(
                                                            "common:toast.copied",
                                                            {
                                                                defaultValue:
                                                                    "Copied to clipboard",
                                                            },
                                                        ),
                                                    );
                                                } catch {
                                                    showStatus(
                                                        "error",
                                                        "Failed to copy",
                                                    );
                                                }
                                            }}
                                            className="inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-2.5 py-1 text-xs font-semibold text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all active:scale-95"
                                            title="Copy SKILL.md content"
                                        >
                                            <svg
                                                width="13"
                                                height="13"
                                                viewBox="0 0 24 24"
                                                fill="none"
                                                stroke="currentColor"
                                                strokeWidth="2"
                                                strokeLinecap="round"
                                                strokeLinejoin="round"
                                            >
                                                <rect
                                                    x="9"
                                                    y="9"
                                                    width="13"
                                                    height="13"
                                                    rx="2"
                                                    ry="2"
                                                />
                                                <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
                                            </svg>
                                            <span>
                                                {t("common:action.copy", {
                                                    defaultValue: "Copy",
                                                })}
                                            </span>
                                        </button>
                                    )}

                                    <button
                                        onClick={closeDetail}
                                        className="rounded-full p-1.5 text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all"
                                    >
                                        ✕
                                    </button>
                                </div>
                            </div>

                            {/* Tabs */}
                            <div className="flex gap-6 border-b border-[var(--border-subtle)] text-xs font-semibold pt-3">
                                <button
                                    type="button"
                                    onClick={() => setActiveTab("body")}
                                    className={`pb-2.5 border-b-2 transition-all ${
                                        activeTab === "body"
                                            ? "border-[var(--accent-gold)] text-[var(--accent-gold)] font-bold"
                                            : "border-transparent text-[var(--text-muted)] hover:text-[var(--text-primary)]"
                                    }`}
                                >
                                    SKILL.md Content
                                </button>
                                <button
                                    type="button"
                                    onClick={() => setActiveTab("files")}
                                    className={`pb-2.5 border-b-2 transition-all ${
                                        activeTab === "files"
                                            ? "border-[var(--accent-gold)] text-[var(--accent-gold)] font-bold"
                                            : "border-transparent text-[var(--text-muted)] hover:text-[var(--text-primary)]"
                                    }`}
                                >
                                    Files List ({detail?.files?.length ?? 0})
                                </button>
                            </div>

                            {/* Modal Body */}
                            <div className="flex-1 overflow-y-auto py-4 min-h-[300px]">
                                {detailLoading && (
                                    <div className="flex items-center justify-center py-16">
                                        <div className="w-6 h-6 border-2 border-[var(--accent-gold)] stroke-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
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
                                    <>
                                        {activeTab === "body" ? (
                                            <div className="space-y-3">
                                                {detail.body && (
                                                    <div className="flex items-center justify-between text-[10px] font-mono text-[var(--text-faint)]">
                                                        <span>YAML Frontmatter + Markdown</span>
                                                        <span>
                                                            {detail.body.split("\n").length} lines · {detail.body.length} chars
                                                        </span>
                                                    </div>
                                                )}
                                                <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-hover)] p-4 overflow-x-auto">
                                                    <pre className="whitespace-pre-wrap text-xs font-mono text-[var(--text-primary)] caret-[var(--accent-gold)] selection:bg-[rgba(245,158,11,0.25)] selection:text-[var(--text-primary)] leading-relaxed">
                                                        {detail.body}
                                                    </pre>
                                                </div>
                                            </div>
                                        ) : (
                                            <div className="space-y-2">
                                                {detail.files && detail.files.length > 0 ? (
                                                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                                                        {detail.files.map((f) => (
                                                            <div
                                                                key={f}
                                                                className="flex items-center gap-2.5 rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-hover)] px-3.5 py-2.5 text-xs font-mono text-[var(--text-secondary)] hover:border-[var(--border-medium)] transition-colors"
                                                            >
                                                                <span className="text-base">📄</span>
                                                                <span className="truncate">{f}</span>
                                                            </div>
                                                        ))}
                                                    </div>
                                                ) : (
                                                    <p className="text-xs text-[var(--text-muted)] py-4 text-center">
                                                        No extra files included
                                                    </p>
                                                )}
                                            </div>
                                        )}
                                    </>
                                )}
                            </div>

                            {/* Modal Footer */}
                            <div className="flex items-center justify-between border-t border-[var(--border-subtle)] pt-4 text-xs">
                                <button
                                    type="button"
                                    onClick={() => handleDelete(detailTarget)}
                                    className="rounded-[var(--radius-sm)] border border-[rgba(244,63,94,0.2)] bg-[rgba(244,63,94,0.05)] px-3.5 py-1.5 text-xs font-semibold text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.15)] hover:border-[var(--accent-coral)] active:scale-95 transition-all"
                                >
                                    {t("common:action.delete", {
                                        defaultValue: "Delete Skill",
                                    })}
                                </button>
                                <button
                                    type="button"
                                    onClick={closeDetail}
                                    className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-4 py-1.5 text-xs font-semibold text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] active:scale-95 transition-all"
                                >
                                    {t("common:action.close", {
                                        defaultValue: "Close",
                                    })}
                                </button>
                            </div>
                        </div>
                    </div>,
                    document.body,
                )}
        </TabPanel>
    );
}
