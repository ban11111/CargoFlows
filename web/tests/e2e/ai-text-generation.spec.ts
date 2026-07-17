import { expect, test, type Page } from "@playwright/test";

const fakeProviderURL = process.env.FAKE_OPENAI_TEST_URL ?? "http://127.0.0.1:8099";

async function loginAsAdmin(page: Page) {
  await page.goto("/login");
  await page.getByLabel("邮箱").fill("admin@cargoflow.local");
  await page.getByLabel("密码").fill("password123");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(/\/skus$/);
}

test("real API and worker complete fake-provider title review and application", async ({ page, request }) => {
  test.setTimeout(120_000);
  const suffix = `${Date.now()}`;
  const templateNameZh = `Lazada 真实链路验收 ${suffix}`;
  const templateNameEn = `Lazada real pipeline ${suffix}`;

  await request.post(`${fakeProviderURL}/__test__/reset`);
  await loginAsAdmin(page);

  await page.goto("/settings/openai");
  await page.getByLabel("OpenAI Project API Key").fill("sk-proj-local-e2e-credential-not-real-00000001");
  await page.getByRole("button", { name: "保存并验证" }).click();
  await expect(page.getByText("凭据已保存并验证")).toBeVisible();

  await page.goto("/ai-templates/new");
  await page.locator("#template-name-zh").fill(templateNameZh);
  await page.locator("#template-name-en").fill(templateNameEn);
  await page.locator("#platform-prompt").fill("为 Lazada 商品生成准确、可搜索且不虚构事实的内容。");
  await page.getByRole("button", { name: "添加标题" }).click();
  await page.locator("#slots\\.0-key").fill("title");
  await page.locator("#slots\\.0-zh").fill("商品标题");
  await page.locator("#slots\\.0-en").fill("Product title");
  await page.locator("#slots\\.0-prompt").fill("生成准确、自然并适合搜索的商品标题。不得虚构商品属性。");
  await page.getByRole("button", { name: "创建草稿" }).click();
  await expect(page.getByRole("button", { name: "草稿已创建" })).toBeDisabled();
  await page.getByRole("button", { name: "运行发布校验" }).click();
  await expect(page.getByText("校验通过，可以发布。")).toBeVisible();
  await page.getByRole("button", { name: "发布版本" }).click();
  await expect(page.getByRole("button", { name: "已发布" })).toBeDisabled();

  await page.goto("/ai-jobs/new");
  await page.locator("#job-sku").selectOption({ label: "CF-CASE-CLR-IP17 · 透明手机壳" });
  await page.locator("#job-template").selectOption({ label: `${templateNameZh} · V1` });
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByRole("checkbox", { name: /商品标题/ }).check();
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByRole("button", { name: "创建任务" }).click();

  await expect(page).toHaveURL(/\/ai-jobs\/[0-9a-f-]+$/);
  const title = page.getByLabel("商品标题");
  await expect(title).toHaveValue(/CargoFlow 透明手机壳 CF-CASE-CLR-IP17/, { timeout: 45_000 });
  await title.fill("CargoFlow 超薄透明手机壳 CF-CASE-CLR-IP17");
  const editResponsePromise = page.waitForResponse((response) => response.request().method() === "PATCH" && response.url().includes("/text-results/"));
  await page.getByRole("button", { name: "保存编辑" }).click();
  const editResponse = await editResponsePromise;
  expect(editResponse.status(), await editResponse.text()).toBe(200);
  await expect(page.getByRole("button", { name: "批准候选" })).toBeEnabled({ timeout: 15_000 });
  await page.getByRole("button", { name: "批准候选" }).click();
  await page.getByRole("button", { name: "预览应用" }).click();
  await expect(page.getByText("应用后 · Revision 1").first()).toBeVisible();
  await page.getByRole("button", { name: "应用到正式内容" }).click();
  await expect(page.getByRole("status")).toContainText("已应用 · Revision 1");

  const providerAudit = await request.get(`${fakeProviderURL}/__test__/requests`);
  expect(providerAudit.ok()).toBe(true);
  const audit = await providerAudit.json() as { data: Array<{ path: string; store: boolean; metadata: Record<string, string>; schema_name: string; contains_forbidden: boolean }> };
  expect(audit.data).toHaveLength(1);
  expect(audit.data[0]).toMatchObject({ path: "/v1/responses", store: false, schema_name: "cargoflow_product_title", contains_forbidden: false });
  expect(Object.keys(audit.data[0].metadata).sort()).toEqual(["execution_id", "job_id", "job_item_id"]);
});
