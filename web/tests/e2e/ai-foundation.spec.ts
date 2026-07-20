import { expect, test, type Page } from "@playwright/test";

async function loginAsAdmin(page: Page) {
  await page.goto("/login");
  await page.getByLabel("邮箱").fill("admin@cargoflows.cc");
  await page.getByLabel("密码").fill("password123");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(/\/skus$/);
}

test("admin can publish a template and complete a deliberately dry-run job", async ({ page }) => {
  test.setTimeout(90_000);
  const suffix = `${Date.now()}`;
  const templateNameZh = `Lazada 标题验收 ${suffix}`;
  const templateNameEn = `Lazada title acceptance ${suffix}`;

  await loginAsAdmin(page);
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
  const titleSlot = page.getByRole("checkbox", { name: /商品标题/ });
  await expect(titleSlot).not.toBeChecked();
  await titleSlot.check();
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByRole("button", { name: "创建任务" }).click();

  await expect(page).toHaveURL(/\/ai-jobs\/[0-9a-f-]+$/);
  const jobHeader = page.getByRole("heading", { name: "CF-CASE-CLR-IP17 · AI 任务" }).locator("..");
  await expect(jobHeader.getByText("已完成", { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("1/1", { exact: true })).toBeVisible();
});
