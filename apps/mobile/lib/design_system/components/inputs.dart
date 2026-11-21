import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../tokens/tokens.dart';
import '../typography/typography.dart';

/// A labelled text field. The label sits above the control (never inside it),
/// hints and errors below, and the error is announced to screen readers.
class MiseTextField extends StatefulWidget {
  const MiseTextField({
    super.key,
    required this.label,
    this.controller,
    this.hint,
    this.errorText,
    this.keyboardType,
    this.textInputAction,
    this.autofillHints,
    this.obscure = false,
    this.onSubmitted,
    this.onChanged,
    this.validator,
    this.maxLines = 1,
    this.maxLength,
    this.enabled = true,
    this.textCapitalization = TextCapitalization.none,
    this.inputFormatters,
    this.focusNode,
  });

  final String label;
  final TextEditingController? controller;
  final String? hint;
  final String? errorText;
  final TextInputType? keyboardType;
  final TextInputAction? textInputAction;
  final Iterable<String>? autofillHints;
  final bool obscure;
  final ValueChanged<String>? onSubmitted;
  final ValueChanged<String>? onChanged;
  final FormFieldValidator<String>? validator;
  final int maxLines;
  final int? maxLength;
  final bool enabled;
  final TextCapitalization textCapitalization;
  final List<TextInputFormatter>? inputFormatters;
  final FocusNode? focusNode;

  @override
  State<MiseTextField> createState() => _MiseTextFieldState();
}

class _MiseTextFieldState extends State<MiseTextField> {
  late bool _hidden = widget.obscure;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(widget.label, style: MiseType.label),
        const SizedBox(height: MiseSpace.xs),
        TextFormField(
          controller: widget.controller,
          focusNode: widget.focusNode,
          enabled: widget.enabled,
          keyboardType: widget.keyboardType,
          textInputAction: widget.textInputAction,
          autofillHints: widget.autofillHints,
          obscureText: _hidden,
          enableSuggestions: !widget.obscure,
          autocorrect: !widget.obscure && widget.keyboardType != TextInputType.emailAddress,
          onFieldSubmitted: widget.onSubmitted,
          onChanged: widget.onChanged,
          validator: widget.validator,
          maxLines: widget.obscure ? 1 : widget.maxLines,
          maxLength: widget.maxLength,
          textCapitalization: widget.textCapitalization,
          inputFormatters: widget.inputFormatters,
          style: MiseType.body,
          decoration: InputDecoration(
            hintText: widget.hint,
            errorText: widget.errorText,
            errorMaxLines: 3,
            counterText: '',
            suffixIcon: widget.obscure
                ? IconButton(
                    tooltip: _hidden ? 'Show password' : 'Hide password',
                    icon: Icon(_hidden ? Icons.visibility_outlined : Icons.visibility_off_outlined, color: MiseColors.muted),
                    onPressed: () => setState(() => _hidden = !_hidden),
                  )
                : null,
          ),
        ),
      ],
    );
  }
}

/// Search field with a clear button. Calls [onChanged] as the user types;
/// debouncing is the caller's job.
class MiseSearchField extends StatelessWidget {
  const MiseSearchField({super.key, required this.controller, required this.onChanged, this.hint = 'Search meals'});

  final TextEditingController controller;
  final ValueChanged<String> onChanged;
  final String hint;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      textField: true,
      label: hint,
      child: ValueListenableBuilder<TextEditingValue>(
        valueListenable: controller,
        builder: (context, value, _) => TextField(
          controller: controller,
          onChanged: onChanged,
          textInputAction: TextInputAction.search,
          style: MiseType.body,
          decoration: InputDecoration(
            hintText: hint,
            prefixIcon: const Icon(Icons.search, color: MiseColors.muted),
            suffixIcon: value.text.isEmpty
                ? null
                : IconButton(
                    tooltip: 'Clear search',
                    icon: const Icon(Icons.close, color: MiseColors.muted),
                    onPressed: () {
                      controller.clear();
                      onChanged('');
                    },
                  ),
          ),
        ),
      ),
    );
  }
}
