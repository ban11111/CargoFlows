import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LanguageProvider } from "@/lib/i18n";
import UsersPage from "./page";

const owner = { public_id: "00000000-0000-4000-8000-000000000001", name: "Owner", email: "owner@example.test", role: "super_admin", status: "active", must_change_password: false, last_seen_at: "2026-07-20T10:00:00Z", created_at: "2026-07-01T10:00:00Z" };
const admin = { ...owner, public_id: "00000000-0000-4000-8000-000000000002", name: "Admin", email: "admin@example.test", role: "admin" };
const operator = { ...owner, public_id: "00000000-0000-4000-8000-000000000003", name: "Operator", email: "operator@example.test", role: "operator", must_change_password: true };
let queryClient: QueryClient;

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={queryClient}>{children}</QueryClientProvider></LanguageProvider>;
}

function response(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }));
}

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
});

describe("Users page", () => {
  it("loads real users, protects the owner, and creates an account with a temporary password", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input);
      if (url.includes("/auth/me")) return response(admin);
      if (url.endsWith("/users") && init?.method === "POST") return response({ ...operator, name: "New operator", email: "new@example.test" }, 201);
      if (url.endsWith("/users")) return response({ data: [owner, admin, operator] });
      return response({}, 404);
    });

    render(<UsersPage />, { wrapper: Providers });
    expect(await screen.findByText("owner@example.test")).toBeInTheDocument();
    expect(screen.getByText("系统主账号")).toBeInTheDocument();
    expect(screen.getByText("待首次改密")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "添加用户" }));
    fireEvent.change(screen.getByLabelText("姓名"), { target: { value: "New operator" } });
    fireEvent.change(screen.getByLabelText("邮箱"), { target: { value: "NEW@example.test" } });
    fireEvent.change(screen.getByLabelText("角色"), { target: { value: "operator" } });
    fireEvent.change(screen.getByLabelText("初始密码"), { target: { value: "temporary-password-123" } });
    fireEvent.change(screen.getByLabelText("确认密码"), { target: { value: "temporary-password-123" } });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/proxy/users", expect.objectContaining({ method: "POST", body: JSON.stringify({ name: "New operator", email: "NEW@example.test", role: "operator", password: "temporary-password-123" }) })));
  });

  it("shows inline validation when temporary passwords do not match", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => String(input).includes("/auth/me") ? response(admin) : response({ data: [owner] }));
    render(<UsersPage />, { wrapper: Providers });
    await screen.findByText("owner@example.test");
    fireEvent.click(screen.getByRole("button", { name: "添加用户" }));
    fireEvent.change(screen.getByLabelText("姓名"), { target: { value: "Mismatch User" } });
    fireEvent.change(screen.getByLabelText("邮箱"), { target: { value: "mismatch@example.test" } });
    fireEvent.change(screen.getByLabelText("初始密码"), { target: { value: "temporary-password-123" } });
    fireEvent.change(screen.getByLabelText("确认密码"), { target: { value: "different-password-456" } });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("两次输入必须一致");
  });

  it("allows a disabled account to be deleted after confirmation", async () => {
    const disabledOperator = { ...operator, status: "disabled" };
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input);
      if (url.includes("/auth/me")) return response(admin);
      if (url.endsWith(`/users/${disabledOperator.public_id}`) && init?.method === "DELETE") return Promise.resolve(new Response(null, { status: 204 }));
      if (url.endsWith("/users")) return response({ data: [owner, admin, disabledOperator] });
      return response({}, 404);
    });

    render(<UsersPage />, { wrapper: Providers });
    expect(await screen.findByText("operator@example.test")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "删除" }));

    expect(confirm).toHaveBeenCalledWith(expect.stringContaining("operator@example.test"));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(`/api/proxy/users/${disabledOperator.public_id}`, expect.objectContaining({ method: "DELETE" })));
  });
});
