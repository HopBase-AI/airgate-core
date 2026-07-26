import eslint from "@eslint/js";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";

export default tseslint.config(
  eslint.configs.recommended,
  ...tseslint.configs.recommendedTypeChecked,
  reactHooks.configs.flat.recommended,
  {
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // 渐进收紧:显式 any 暂不禁,但 any 的不安全"使用"由 no-unsafe-* 把守
      "@typescript-eslint/no-explicit-any": "off",
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
      // 以下存量较多,先降 warn 做可见债;清零一档提一档 error
      "@typescript-eslint/no-unsafe-member-access": "warn",
      "@typescript-eslint/no-unsafe-assignment": "warn",
      "@typescript-eslint/no-floating-promises": "warn",
      "@typescript-eslint/no-misused-promises": "warn",
      "@typescript-eslint/no-non-null-assertion": "warn",
      "@typescript-eslint/only-throw-error": "warn",
      "@typescript-eslint/no-base-to-string": "warn",
      "@typescript-eslint/no-redundant-type-constituents": "warn",
      // react-hooks v7 的 React Compiler 系规则:有存量的降 warn(专项治理再升),
      // 零存量的保持默认 error 挡新增
      "react-hooks/set-state-in-effect": "warn",
      "react-hooks/refs": "warn",
      "react-hooks/purity": "warn",
      "react-hooks/immutability": "warn",
      "react-hooks/preserve-manual-memoization": "warn",
    },
  },
  { ignores: ["dist/"] },
);
