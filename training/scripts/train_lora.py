#!/usr/bin/env python3
"""
Hungarian Customer Service Tool-Calling LoRA Fine-Tuning Script
Target model: Qwen/Qwen2.5-1.5B-Instruct, Qwen/Qwen2.5-0.5B-Instruct, or Qwen/Qwen2.5-3B-Instruct
Uses 4-bit QLoRA to fit comfortably within 4GB VRAM (NVIDIA Quadro T2000).
"""

import argparse
import os
os.environ["HF_HUB_DISABLE_XET"] = "1"
import torch
from datasets import load_dataset
from transformers import (
    AutoModelForCausalLM,
    AutoTokenizer,
    BitsAndBytesConfig,
    TrainingArguments
)
from peft import LoraConfig, get_peft_model, prepare_model_for_kbit_training
from trl import SFTTrainer

def parse_args():
    parser = argparse.ArgumentParser(description="QLoRA fine-tuning for Hungarian customer service tool-calling.")
    parser.add_argument("--model", type=str, default="Qwen/Qwen2.5-1.5B-Instruct", help="Hugging Face model ID or local path")
    parser.add_argument("--dataset", type=str, default="training/dataset/sample_hungarian_customer_service.jsonl", help="Path to JSONL dataset")
    parser.add_argument("--output-dir", type=str, default="training/output/qwen-hu-lora", help="Output directory for LoRA adapters")
    parser.add_argument("--max-steps", type=int, default=60, help="Maximum number of training steps")
    parser.add_argument("--batch-size", type=int, default=1, help="Per-device batch size")
    parser.add_argument("--grad-accum", type=int, default=4, help="Gradient accumulation steps")
    parser.add_argument("--lr", type=float, default=2e-4, help="Learning rate")
    parser.add_argument("--max-seq-len", type=int, default=512, help="Maximum sequence length")
    parser.add_argument("--dry-run", action="store_true", help="Run 2 steps to verify training loop and VRAM")
    return parser.parse_args()

def main():
    args = parse_args()

    max_steps = 2 if args.dry_run else args.max_steps
    print(f"Loading base model: {args.model} with 4-bit QLoRA...")

    # 1. 4-bit NF4 Quantization configuration for low VRAM (4GB Quadro T2000)
    bnb_config = BitsAndBytesConfig(
        load_in_4bit=True,
        bnb_4bit_quant_type="nf4",
        bnb_4bit_compute_dtype=torch.float16,
        bnb_4bit_use_double_quant=True
    )

    tokenizer = AutoTokenizer.from_pretrained(args.model, trust_remote_code=True)
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token

    model = AutoModelForCausalLM.from_pretrained(
        args.model,
        quantization_config=bnb_config,
        device_map="auto",
        trust_remote_code=True
    )
    model = prepare_model_for_kbit_training(model)

    # 2. LoRA Adapter configuration targeting attention and MLP projections
    lora_config = LoraConfig(
        r=16,
        lora_alpha=32,
        target_modules=["q_proj", "k_proj", "v_proj", "o_proj", "gate_proj", "up_proj", "down_proj"],
        lora_dropout=0.05,
        bias="none",
        task_type="CAUSAL_LM"
    )
    model = get_peft_model(model, lora_config)
    model.print_trainable_parameters()

    # 3. Load and format dataset using Qwen chat template
    print(f"Loading dataset from: {args.dataset}")
    dataset = load_dataset("json", data_files=args.dataset, split="train")

    def format_chat(batch):
        formatted = []
        for msgs in batch["messages"]:
            text = tokenizer.apply_chat_template(msgs, tokenize=False)
            formatted.append(text)
        return {"text": formatted}

    formatted_dataset = dataset.map(format_chat, batched=True)

    # 4. Training arguments optimized for 4GB VRAM
    training_args = TrainingArguments(
        output_dir=args.output_dir,
        per_device_train_batch_size=args.batch_size,
        gradient_accumulation_steps=args.grad_accum,
        warmup_steps=2 if args.dry_run else 5,
        max_steps=max_steps,
        learning_rate=args.lr,
        fp16=True,
        logging_steps=1 if args.dry_run else 5,
        save_strategy="no" if args.dry_run else "steps",
        save_steps=30,
        optim="paged_adamw_8bit",
        report_to="none"
    )

    try:
        trainer = SFTTrainer(
            model=model,
            train_dataset=formatted_dataset,
            dataset_text_field="text",
            max_seq_length=args.max_seq_len,
            processing_class=tokenizer,
            args=training_args,
        )
    except TypeError:
        trainer = SFTTrainer(
            model=model,
            train_dataset=formatted_dataset,
            dataset_text_field="text",
            max_seq_length=args.max_seq_len,
            tokenizer=tokenizer,
            args=training_args,
        )

    print(f"Starting QLoRA fine-tuning ({max_steps} steps)...")
    trainer.train()

    if not args.dry_run:
        print(f"Training completed successfully! Saving LoRA adapter to: {args.output_dir}")
        trainer.model.save_pretrained(args.output_dir)
        tokenizer.save_pretrained(args.output_dir)
    else:
        print("Dry run completed successfully! Memory and backward pass verified.")

if __name__ == "__main__":
    main()
