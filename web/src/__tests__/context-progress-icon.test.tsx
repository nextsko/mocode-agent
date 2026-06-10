import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { ContextProgressIcon } from "@ai-elements";

describe("ContextProgressIcon", () => {
  it("renders an SVG with accessibility attributes", () => {
    const { container } = render(<ContextProgressIcon usedPercent={0.5} />);
    const svg = container.querySelector("svg");
    expect(svg).toBeInTheDocument();
    expect(svg).toHaveAttribute("aria-label", "Model context usage");
    expect(svg).toHaveAttribute("role", "img");
  });

  it("renders two circles (background + progress)", () => {
    const { container } = render(<ContextProgressIcon usedPercent={0.5} />);
    const circles = container.querySelectorAll("circle");
    expect(circles).toHaveLength(2);
  });

  it("uses the provided size", () => {
    const { container } = render(
      <ContextProgressIcon usedPercent={0.5} size={32} />,
    );
    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("width", "32");
    expect(svg).toHaveAttribute("height", "32");
  });

  it("defaults to size 20 when not specified", () => {
    const { container } = render(<ContextProgressIcon usedPercent={0.5} />);
    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("width", "20");
    expect(svg).toHaveAttribute("height", "20");
  });

  it("handles 0% usage (full circle offset)", () => {
    const { container } = render(<ContextProgressIcon usedPercent={0} />);
    const progressCircle = container.querySelectorAll("circle")[1];
    // strokeDashoffset should equal circumference (full gap = no visible arc)
    const r = 10;
    const circumference = 2 * Math.PI * r;
    expect(progressCircle).toHaveAttribute(
      "stroke-dashoffset",
      circumference.toString(),
    );
  });

  it("handles 100% usage (zero offset)", () => {
    const { container } = render(<ContextProgressIcon usedPercent={1} />);
    const progressCircle = container.querySelectorAll("circle")[1];
    expect(progressCircle).toHaveAttribute("stroke-dashoffset", "0");
  });

  it("clamps NaN to 0%", () => {
    const { container } = render(
      <ContextProgressIcon usedPercent={Number.NaN} />,
    );
    const progressCircle = container.querySelectorAll("circle")[1];
    const r = 10;
    const circumference = 2 * Math.PI * r;
    expect(progressCircle).toHaveAttribute(
      "stroke-dashoffset",
      circumference.toString(),
    );
  });

  it("clamps negative values to 0%", () => {
    const { container } = render(
      <ContextProgressIcon usedPercent={-0.5} />,
    );
    const progressCircle = container.querySelectorAll("circle")[1];
    const r = 10;
    const circumference = 2 * Math.PI * r;
    expect(progressCircle).toHaveAttribute(
      "stroke-dashoffset",
      circumference.toString(),
    );
  });

  it("clamps values above 1 to 100%", () => {
    const { container } = render(
      <ContextProgressIcon usedPercent={1.5} />,
    );
    const progressCircle = container.querySelectorAll("circle")[1];
    expect(progressCircle).toHaveAttribute("stroke-dashoffset", "0");
  });
});
