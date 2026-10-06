import MarkdownIt from "markdown-it";
import { highlightCode } from "./code-highlight";
const markdown = new MarkdownIt({
  html: false,
  linkify: true,
  breaks: true,
  highlight: highlightCode,
});
const defaultLink = markdown.renderer.rules.link_open;
markdown.renderer.rules.link_open = (tokens, index, options, environment, renderer) => {
  tokens[index]!.attrSet("rel", "noopener noreferrer");
  return defaultLink ? defaultLink(tokens, index, options, environment, renderer) : renderer.renderToken(tokens, index, options);
};
export function renderSafeMarkdown(source: string): string {
  return markdown.render(source);
}
